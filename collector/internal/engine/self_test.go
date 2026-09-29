package engine

import (
	"os/exec"
	"testing"
	"time"
)

// The first tick reads the whole inventory once; it is not what a sample
// costs, so it stays out of the average.
func TestSelfAverageLeavesOutTheFirstTick(t *testing.T) {
	var m selfMeter
	if tick, avg := m.add(96); tick != 96 || avg != 96 {
		t.Errorf("first tick: tick %v avg %v, want 96 96 (nothing else to average yet)", tick, avg)
	}
	m.add(2)
	if _, avg := m.add(4); avg != 3 {
		t.Errorf("avg = %v, want 3 (the first tick left out)", avg)
	}
}

func TestSelfAverageKeepsThirtyTicks(t *testing.T) {
	var m selfMeter
	m.add(100)
	for i := 0; i < 30; i++ {
		m.add(10)
	}
	if _, avg := m.add(40); avg != 11 {
		t.Errorf("avg = %v, want 11 (last 30: 29×10 + 40)", avg)
	}
}

// ss y nvidia-smi son hijos del colector: su CPU es parte de lo que cuesta
// una muestra (RUSAGE_CHILDREN, al esperarlos).
func TestSelfCostCountsChildren(t *testing.T) {
	var m selfMeter
	m.add(0)
	start := cpuTime()
	cmd := exec.Command("sh", "-c", "i=0; while [ $i -lt 30000 ]; do i=$((i+1)); done")
	if err := cmd.Run(); err != nil {
		t.Skip("no sh:", err)
	}
	child := cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()
	if child < 20*time.Millisecond {
		t.Skipf("the child used only %v", child)
	}
	tick, _ := m.measure(start)
	if want := float64(child) / float64(time.Millisecond); tick < want*0.9 {
		t.Fatalf("tick = %.1f ms, want at least the child's %.1f ms", tick, want)
	}
}
