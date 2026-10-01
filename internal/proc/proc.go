// Package proc reports which processes are running and how they are related.
package proc

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// maxAscent bounds a walk toward pid 1.
const maxAscent = 12

type Process struct {
	PID     int
	PPID    int
	Command string
}

type Table map[int]Process

// Snapshot reads every running process in one pass. It can fail or come back
// empty under a sandbox, which callers must treat as "cannot tell", not "nothing running".
func Snapshot() (Table, error) {
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return nil, err
	}
	table := Table{}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 3 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		table[pid] = Process{PID: pid, PPID: ppid, Command: strings.Join(fields[2:], " ")}
	}
	return table, nil
}

// Running reports whether pid is live and its command still looks like want,
// guarding against a recycled pid. An empty want checks existence only.
func (t Table) Running(pid int, want string) bool {
	p, ok := t[pid]
	if !ok {
		return false
	}
	if want == "" {
		return true
	}
	return strings.Contains(filepath.Base(p.Command), want)
}

// Ancestors lists pid and its forebears, nearest first.
func (t Table) Ancestors(pid int) []int {
	out := []int{pid}
	for i := 0; i < maxAscent; i++ {
		p, ok := t[pid]
		if !ok || p.PPID <= 1 {
			break
		}
		out = append(out, p.PPID)
		pid = p.PPID
	}
	return out
}

// NearestMatch returns the closest ancestor of pid whose command looks like
// want, or 0 when there is none.
func (t Table) NearestMatch(pid int, want string) int {
	for _, candidate := range t.Ancestors(pid) {
		if t.Running(candidate, want) {
			return candidate
		}
	}
	return 0
}
