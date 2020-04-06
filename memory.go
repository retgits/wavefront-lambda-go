package wflambda

import "github.com/shirou/gopsutil/mem"

// memStats contains usage statistics. Total and Used contain numbers of megabytes for human
// consumption and UsedPercentage contains a percentage value.
type memStats struct {
	Total          float64
	Used           float64
	UsedPercentage float64
}

// getMemoryStats retrieves memory statistics from the container the AWS Lambda function runs
// in. It returns stats for Total and Used (numbers of megabytes for human consumption) and
// UsedPercentage (a percentage value).
func getMemoryStats() *memStats {
	stats, _ := mem.VirtualMemory()
	return &memStats{
		Total:          float64(stats.Total) / float64(1<<20),
		Used:           float64(stats.Used) / float64(1<<20),
		UsedPercentage: stats.UsedPercent,
	}
}
