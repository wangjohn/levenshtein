package assertions

import (
	"log"
	"os"
	"regexp"
	"testing"
)

func TestCallsOnly(t *testing.T) { // want "TestCallsOnly has no assertion"
	Double(2)
}

func TestLogsOnly(t *testing.T) { // want "TestLogsOnly has no assertion"
	t.Parallel()
	t.Logf("double is %d", Double(2))
}

func TestEmpty(t *testing.T) {} // want "TestEmpty has no assertion"

func TestQuietSubtest(t *testing.T) { // want "TestQuietSubtest has no assertion"
	t.Run("double", func(t *testing.T) {
		t.Log(Double(2))
	})
}

func TestDiscardsResult(t *testing.T) { // want "TestDiscardsResult has no assertion"
	got := Double(2)
	_ = got
	t.Cleanup(func() { t.Log("done") })
}

func TestAlwaysSkips(t *testing.T) {
	t.Skip("flaky") // want "TestAlwaysSkips always skips"
	if Double(2) != 4 {
		t.Fatal("wrong")
	}
}

func TestSkipsAfterQuietSetup(t *testing.T) {
	t.Parallel()
	t.Log("setup")
	t.Skip("todo") // want "TestSkipsAfterQuietSetup always skips"
}

func TestSkipsAfterCheck(t *testing.T) {
	if Double(2) != 4 {
		t.Fatal("wrong")
	}
	t.Skip("rest is todo")
}

func TestSkipsAfterEarlyReturn(t *testing.T) { // want "TestSkipsAfterEarlyReturn has no assertion"
	if testing.Short() {
		return
	}
	t.Skip("slow")
}

func TestSkipsAfterGoto(t *testing.T) { // want "TestSkipsAfterGoto has no assertion"
	if testing.Short() {
		goto done
	}
	t.Skip("slow")
done:
}

func TestOnlyCallsMustFunction(t *testing.T) { // want "TestOnlyCallsMustFunction has no assertion"
	regexp.MustCompile(`^[a-z]+$`)
}

func TestStructKeyOnly(t *testing.T) { // want "TestStructKeyOnly has no assertion"
	_ = harness{t: nil}
}

func TestLogFatal(t *testing.T) {
	if Double(2) != 4 {
		log.Fatal("wrong")
	}
}

func TestLoggerPanics(t *testing.T) {
	logger := log.New(os.Stderr, "", 0)
	if Double(2) != 4 {
		logger.Panicf("Double(2) = %d", Double(2))
	}
}

func TestExits(t *testing.T) {
	if Double(2) != 4 {
		os.Exit(1)
	}
}

func TestAsserts(t *testing.T) {
	if got := Double(2); got != 4 {
		t.Errorf("Double(2) = %d, want 4", got)
	}
}

func TestAssertsInSubtest(t *testing.T) {
	t.Run("double", func(t *testing.T) {
		if Double(2) != 4 {
			t.Fatal("wrong")
		}
	})
}

func TestNamedSubtest(t *testing.T) {
	t.Run("double", checkDouble)
}

func checkDouble(t *testing.T) {
	if Double(2) != 4 {
		t.Fatal("wrong")
	}
}

func TestHelper(t *testing.T) {
	expectDouble(t, 2, 4)
}

func expectDouble(tb testing.TB, in, want int) {
	tb.Helper()
	if got := Double(in); got != want {
		tb.Errorf("Double(%d) = %d, want %d", in, got, want)
	}
}

func TestPassesToStruct(t *testing.T) {
	h := harness{t: t}
	h.check()
}

type harness struct {
	t *testing.T
}

func (h harness) check() {
	if Double(1) != 2 {
		h.t.Fatal("wrong")
	}
}

func TestPanics(t *testing.T) {
	if Double(2) != 4 {
		panic("wrong")
	}
}

func TestSkipsConditionally(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	if Double(2) != 4 {
		t.Fatal("wrong")
	}
}

func TestMain(m *testing.M) {
	m.Run()
}

func Testlowercase(t *testing.T) {}

func BenchmarkDouble(b *testing.B) {
	for b.Loop() {
		Double(2)
	}
}
