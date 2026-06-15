package render

import "testing"

func TestValidate(t *testing.T) {
	valid := `scene validate_ok

path s:
  at: [0, 0]
  points: [[-0.5, -0.5], [0.5, -0.5], [0.5, 0.5], [-0.5, 0.5]]
  closed: 1
  stroke.color: color.white

| 1s | s{draw: 0} -> s
`
	if err := Validate(valid); err != nil {
		t.Fatalf("Validate(valid): %v", err)
	}

	if err := Validate("not pdtt"); err == nil {
		t.Fatal("Validate(invalid) should fail")
	}
}
