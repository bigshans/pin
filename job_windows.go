//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// A job object is how Windows expresses "this process and everything it
// spawns". Joining one is the only reliable way to kill a console tool
// *together with its grandchildren* — pin -> node(ni) -> cmd.exe(npm) ->
// node(script) is a four deep chain, and killing the first process leaves the
// rest alive.
//
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE makes it unconditional: when the last
// handle to the job closes, every process in it dies. That covers the cases we
// cannot handle in Go at all — the user closes the terminal window, or pin
// itself is killed with Task Manager — because Windows closes our handles for
// us on the way out.
//
// Everything below is left to you. Nothing outside this file needs to change:
// exec_windows.go already creates a job before spawning the tool, assigns the
// child immediately after Start, and falls back to `taskkill /T /F` when
// newKillOnCloseJob reports an error.
//
// Win32 entry points, all in kernel32.dll (do not add a dependency on
// golang.org/x/sys/windows — this project has none):
//
//	CreateJobObjectW(nil, nil)                       -> (handle, errno)
//	SetInformationJobObject(job, 9, &info, size)     -> (BOOL, errno)
//	AssignProcessToJobObject(job, processHandle)     -> (BOOL, errno)
//	TerminateJobObject(job, 1)                       -> (BOOL, errno)
//	CloseHandle(handle)
//
// plus syscall.OpenProcess / syscall.CloseHandle, already in the stdlib.
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
// 144 bytes total on amd64. Go's alignment rules match C here as long as you
// keep the field order and use the widths above — but do not trust that, run
// the test.
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

var errJobUnsupported = errors.New("job objects are not implemented yet")

// newKillOnCloseJob creates an unnamed job with KILL_ON_JOB_CLOSE set.
func newKillOnCloseJob() (*job, error) {
	return nil, errJobUnsupported
}

// assign puts p — and every process p spawns from now on — into the job.
//
// Timing is the interesting part. A process created before the assignment is
// not in the job, so this has to run as soon as Start returns: the window is
// the time the child needs to boot and spawn children of its own, which is
// tens of milliseconds for node and effectively zero for cmd.exe. There is no
// way to close it completely through os/exec (a fully correct implementation
// would start the child with CREATE_SUSPENDED, assign, then ResumeThread, and
// os/exec does not expose the thread handle). Write down what you chose.
func (j *job) assign(p *os.Process) error {
	return errJobUnsupported
}

// terminate kills every process in the job.
func (j *job) terminate() error {
	return errJobUnsupported
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
