package process

import (
	"bufio"
	"container/heap"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	totalMem uint64
)

// NewProcessMonitor returns a new process monitor.
// n: the number of processes to return, interval: the interval of data collection.
func NewProcessMonitor(n, interval int) *ProcessMonitor {
	info := &syscall.Sysinfo_t{}
	syscall.Sysinfo(info)
	totalMem = info.Totalram * uint64(info.Unit)
	monitor := &ProcessMonitor{
		last:           make(map[string]*LinuxProcess),
		current:        make(map[string]*LinuxProcess),
		lastUpdate:     time.Now(),
		updateInterval: interval,
		top:            make(TopHeap, 0),
		size:           n,
		topNCache:      make([]*ProcessMetric, n),
		done:           make(chan struct{}),
	}
	return monitor
}

// Run runs the monitor and returns if Stop is called.
func (m *ProcessMonitor) Run() {
	log.Println("process monitor is running")
	ticker := time.NewTicker(time.Second * time.Duration(m.updateInterval))
	for {
		m.update()
		select {
		case <-m.done:
			return
		case <-ticker.C:
		}
	}
}

// Stop stops the monitor
func (m *ProcessMonitor) Stop() {
	close(m.done)
}

// GetTopN returns a slice of top-N processes sorted by each's cpu usage
func (m *ProcessMonitor) GetTopN() []*ProcessMetric {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return m.topNCache
}

func (m *ProcessMonitor) update() {
	files, err := ioutil.ReadDir("/proc")
	if err != nil {
		log.Println(err)
		return
	}
	for _, f := range files {
		_, err := strconv.ParseInt(f.Name(), 10, 64)
		if f.IsDir() && err == nil {
			m.updateProc(f.Name())
		}
	}
	updatedAt := time.Now()
	m.calculateUsage()
	m.lastUpdate = updatedAt
	// for next update
	l := make(map[string]*LinuxProcess)
	for k, v := range m.current {
		l[k] = v
	}
	m.last = l
	m.updateCache()
}

func (m *ProcessMonitor) calculateUsage() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.top = make([]*ProcessMetric, 0)
	heap.Init(&m.top)
	for currentPID, currentProc := range m.current {
		if lastProc, ok := m.last[currentPID]; ok {
			deltaTime := currentProc.CPUTime() - lastProc.CPUTime()
			duration := time.Since(m.lastUpdate).Nanoseconds() / 1e9
			usage := 100 * float64(deltaTime) / float64(duration)
			res := currentProc.ResidentMemory()
			memUsage := 100 * float64(res) / float64(totalMem)
			pd := &ProcessMetric{
				PID:    currentPID,
				Comm:   currentProc.comm,
				State:  currentProc.state,
				CPU:    usage,
				Memory: memUsage,
			}
			heap.Push(&m.top, pd)
		}
	}
}

func (m *ProcessMonitor) updateCache() {
	var count int
	var processes []*ProcessMetric
	top := m.top
	for top.Len() > 0 {
		p := heap.Pop(&top).(*ProcessMetric)
		processes = append(processes, p)
		count++
		if count == m.size {
			break
		}
	}
	m.mtx.Lock()
	m.topNCache = processes
	m.mtx.Unlock()
}

func (m *ProcessMonitor) updateProc(pid string) {
	procFile := fmt.Sprintf("/proc/%s/stat", pid)
	file, err := os.Open(procFile)
	if err != nil {
		log.Println(err)
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		ut, err := strconv.Atoi(parts[13])
		if err != nil {
			log.Println(err)
			continue
		}
		st, err := strconv.Atoi(parts[14])
		if err != nil {
			log.Println(err)
			continue
		}
		rss, err := strconv.Atoi(parts[23])
		if err != nil {
			log.Println(err)
			continue
		}
		p := &LinuxProcess{
			pid:   parts[0],
			comm:  strings.Trim(parts[1], "()"),
			state: parts[2],
			utime: ut,
			stime: st,
			rss:   rss,
		}
		m.current[p.pid] = p
	}
}

type ProcessMonitor struct {
	// Last time collected process data.
	last map[string]*LinuxProcess
	// Current collected process data.
	current map[string]*LinuxProcess
	// Last update time.
	lastUpdate time.Time
	// Interval if collection, in seconds.
	updateInterval int
	// The heap storing sorted processes, updates every interval.
	top TopHeap
	// The size of the heap.
	size int
	// The monitor data of the top N processes.
	topNCache []*ProcessMetric
	mtx       sync.Mutex
	done      chan struct{}
}

type LinuxProcess struct {
	// The Process ID.
	pid string
	// The filename of the executable.
	comm string
	// The process state.
	state string
	// Amount of time that this process has been scheduled
	// in user mode, measured in clock ticks.
	utime int
	// Amount of time that this process has been scheduled
	// in kernel mode, measured in clock ticks.
	stime int
	// Resident Set Size: number of pages the process has
	// in real memory.
	rss int
}

func (p *LinuxProcess) ResidentMemory() int {
	return p.rss * os.Getpagesize()
}

func (p *LinuxProcess) CPUTime() float64 {
	return float64(p.utime+p.stime) / 100
}

type ProcessMetric struct {
	PID    string
	Comm   string
	State  string
	CPU    float64
	Memory float64
}

type TopHeap []*ProcessMetric

func (h TopHeap) Len() int { return len(h) }

func (h TopHeap) Less(i, j int) bool { return (*h[i]).CPU > (*h[j]).CPU }
func (h TopHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *TopHeap) Push(x interface{}) {
	*h = append(*h, x.(*ProcessMetric))
}

func (h *TopHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
