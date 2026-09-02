// Package unfailable holds tests that CANNOT FAIL, on purpose.
//
// It lives under testdata so the analyzer's own skipDirs keeps it out of the
// repository inventory: a fixture and a finding must never be confusable.
package unfailable

import "testing"

// Runs, and nothing in it can fail.
func TestNothingHappens(t *testing.T) {
	x := 1 + 1
	_ = x
	t.Log("looks busy")
}

// The table is empty, so the body never runs. This is the gotests scaffold.
func TestEmptyTable(t *testing.T) {
	tests := []struct{ name string }{}
	for _, tt := range tests {
		if tt.name == "" {
			t.Fatal("unreachable")
		}
	}
}

// The worst shape: a real case, the code called, nothing checked. It executes,
// so it produces coverage and a green PASS.
func TestPopulatedButMute(t *testing.T) {
	tests := []struct {
		name string
		in   int
	}{
		{name: "a case", in: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = double(tt.in)
		})
	}
}

func double(n int) int { return n * 2 }

// Names a behaviour and declines to test it, permanently.
func TestUnconditionalSkip(t *testing.T) {
	t.Skip("requires terraform state machinery")
}

// An empty map that is never written and then ranged over is a loop that never
// runs, exactly like an unfilled slice table. This is the case a slice-only
// rule would have missed, which is why the rule is "never written after"
// rather than "must be a slice".
func TestEmptyMapNeverWritten(t *testing.T) {
	byName := map[string]int{}
	for name, count := range byName {
		if count != 0 {
			t.Fatalf("%s: unreachable", name)
		}
	}
}

// A table declared with var and an empty composite literal -- the gotests
// scaffold most people actually write -- not the := form the assignment
// check already covers. The loop still never runs.
func TestVarDeclaredEmptyTable(t *testing.T) {
	var cases = []struct{ name string }{}
	for _, tt := range cases {
		t.Errorf("unreachable: %s", tt.name)
	}
}

// The donation channel: verify exists on two types, one asserting and one
// mute, and this test calls the mute one. A resolver that looks methods up
// by bare name finds donor.verify and inherits its verdict -- exactly how
// three device schema tests passed for months by reaching a logger's Error.
func TestMethodOnUnrelatedType(t *testing.T) {
	m := mute{}
	m.verify()
}

// The receiver's declared type is an interface, so no method body is
// resolvable from syntax. The call must be judged non-asserting even though
// a same-named method that asserts exists in the package.
func TestUnresolvableReceiver(t *testing.T) {
	var v verifier = mute{}
	v.verify()
}

// One name, two types, and the mute binding is the one called. Which
// declaration a given call sees is a scope question the parser cannot
// answer, so the name resolves to nothing.
func TestShadowedReceiver(t *testing.T) {
	d := donor{t: t}
	_ = d
	{
		d := mute{}
		d.verify()
	}
}

type donor struct{ t *testing.T }

func (d donor) verify() { d.t.Fatal("boom") }

type mute struct{}

func (m mute) verify() {}

type verifier interface{ verify() }

// A bare call is a free function, never a method: the free doNothing is what
// runs here, and the asserting method of the same name must not stand in
// for it.
func TestBareNameIsNotAMethod(t *testing.T) {
	doNothing()
}

func doNothing() {}

func (d donor) doNothing() { d.t.Fatal("boom") }
