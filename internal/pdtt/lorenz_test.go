package pdtt

import (
	"testing"
)

func TestLorenzMemoSameTAllAxes(t *testing.T) {
	lorenzMemo = map[lorenzMemoKey][3]float64{}
	tval, rho, seed := 3.7, 14.0, 1.0
	x1, y1, z1 := lorenzAt(tval, rho, seed)
	x2, y2, z2 := lorenzAt(tval, rho, seed)
	if x1 != x2 || y1 != y2 || z1 != z2 {
		t.Fatalf("memo should return identical results for same args")
	}
}

func TestLorenzMemoExactKeys(t *testing.T) {
	lorenzMemo = map[lorenzMemoKey][3]float64{}
	x1, _, _ := lorenzAt(0.15333333333333335, 14, 0)
	x2, _, _ := lorenzAt(0.15, 14, 0)
	if x1 == x2 {
		t.Fatalf("nearby t values must not share rounded memo buckets")
	}
}
