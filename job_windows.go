//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// A job object is how Windows expresses "this process and everything it
// spawns". It is the only reliable way to kill a console tool *together with
// its grandchildren*: pin -> node(ni) -> cmd.exe(npm) -> node(script) is a four
// deep chain, and Ctrl+C only reaches it because the console broadcasts, which
// stops being true the moment pin itself is killed first.
//
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE covers exactly that gap: when the last
// handle to the job closes, every process in it dies. Closing the terminal
// window or killing pin from Task Manager looks, from inside Go, like nothing
// at all — Windows closes our handles on the way out, and the kernel takes it
// from there.
//
// kernel32 is reached through syscall.NewLazyDLL rather than
// golang.org/x/sys/windows: this project has no dependencies.
//
// The structure passed to SetInformationJobObject is
// JOBOBJECT_EXTENDED_LIMIT_INFORMATION (class 9). In C it is laid out as:
//
//	JOBOBJECT_BASIC_LIMIT_INFORMATION BasicLimitInformation;  // 64 bytes
//	    LARGE_INTEGER PerProcessUserTimeLimit;   // int64
//	    LARGE_INTEGER PerJobUserTimeLimit;       // int64
//	    DWORD         LimitFlags;                // uint32  <- set 0x2000 here
//	    SIZE_T        MinimumWorkingSetSize;     // uintptr (8-byte aligned!)
//	    SIZE_T        MaximumWorkingSetSize;     // uintptr
//	    DWORD         ActiveProcessLimit;        // uint32
//	    ULONG_PTR     Affinity;                  // uintptr (8-byte aligned!)
//	    DWORD         PriorityClass;             // uint32
//	    DWORD         SchedulingClass;           // uint32
//	IO_COUNTERS IoInfo;                          // 6 * uint64 = 48 bytes
//	SIZE_T ProcessMemoryLimit;                   // uintptr
//	SIZE_T JobMemoryLimit;                       // uintptr
//	SIZE_T PeakProcessMemoryUsed;                // uintptr
//	SIZE_T PeakJobMemoryUsed;                    // uintptr
//
// 144 bytes on 64-bit. Go's padding happens to match MSVC's here, but keep the
// assertion below: it is what holds the struct to that claim.
type job struct {
	h uintptr
}

// kernel32 entry points, resolved once. syscall.NewLazyDLL is the zero-dependency
// way in: no cgo, no golang.org/x/sys/windows.
var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procCloseHandle              = kernel32.NewProc("CloseHandle")
)

// Values straight out of the Windows SDK headers; syscall does not export them.
const (
	jobObjectExtendedLimitInformation = 9      // JOBOBJECTINFOCLASS
	jobObjectLimitKillOnJobClose      = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	processSetQuota                   = 0x0100 // PROCESS_SET_QUOTA
	processTerminate                  = 0x0001 // PROCESS_TERMINATE
)

// The C structs, field for field, in declaration order. Only LimitFlags is set;
// everything else stays zero. Go's rules happen to produce the same padding as
// MSVC here, but the assertion at the bottom is what actually holds us to it.
type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type basicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr // 8-byte aligned: the uint32 above leaves a hole
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr // same hole again
	PriorityClass           uint32
	SchedulingClass         uint32
}

type extendedLimitInformation struct {
	BasicLimitInformation basicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// Compile-time proof that the Go struct matches the C layout on 64-bit. If you
// change a field type or order, this stops building instead of silently telling
// the kernel the wrong thing.
var _ [144 - int(unsafe.Sizeof(extendedLimitInformation{}))]byte
var _ [int(unsafe.Sizeof(extendedLimitInformation{})) - 144]byte

// newKillOnCloseJob creates an unnamed job with KILL_ON_JOB_CLOSE set.
func newKillOnCloseJob() (*job, error) {
	h, _, err := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return nil, fmt.Errorf("CreateJobObject: %w", err)
	}
	info := extendedLimitInformation{}
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r, _, err := procSetInformationJobObject.Call(h, jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if r == 0 {
		procCloseHandle.Call(h)
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	return &job{h: h}, nil
}

// assign puts p — and every process p spawns from now on — into the job.
//
// Timing is the interesting part: a process created before the assignment is
// not in the job, so this runs as soon as Start returns. The remaining window
// is however long the child needs to spawn children of its own — tens of
// milliseconds for node, effectively zero for cmd.exe. It is accepted rather
// than closed: closing it needs the thread handle from CREATE_SUSPENDED, which
// os/exec does not expose, and the only tool that would benefit is one that
// forks within microseconds of being exec'd.
func (j *job) assign(p *os.Process) error {
	h, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(p.Pid))
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	r, _, err := procAssignProcessToJobObject.Call(j.h, uintptr(h))
	if r == 0 {
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return nil
}

// terminate kills every process in the job.
func (j *job) terminate() error {
	r, _, err := procTerminateJobObject.Call(j.h, 1)
	if r == 0 {
		return fmt.Errorf("TerminateJobObject: %w", err)
	}
	return nil
}

// close releases the handle. The processes are already gone by then; if any
// survived (a tool that daemonised) this is what kills them.
func (j *job) close() {
	if j == nil || j.h == 0 {
		return
	}
	procCloseHandle.Call(j.h)
	j.h = 0
}
