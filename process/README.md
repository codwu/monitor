# process monitor

The process monitor can monitor processes in your Linux host. It sorts the processes by CPU usage. You can get top N processes' metrics including PID, state, command, CPU and Memory usage. 

## example

```
func main() {
	mo := process.NewProcessMonitor(5,3)
	go mo.Run()
	time.Sleep(time.Second*10)
	processes := mo.GetTopN()
	for _,p := range processes {
		fmt.Printf("pid: %s state: %s CPU: %.2f  MEM: %.2f  Com: %s\n", p.PID, p.State, p.CPU, p.Memory, p.Comm)
	}
	mo.Stop()
}
```