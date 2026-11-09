package agent

import (
	"math"
	"testing"
	"time"
)

// a clock the test moves by hand
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func near(got, want, tolerance float64) bool { return math.Abs(got-want) <= tolerance }

// ten tokens every 200 ms is fifty a second, read from the stream alone
func TestPaceMeasuresDecodeSpeedFromWhatArrives(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	p := newPace(clock.now, 4)
	clock.advance(500 * time.Millisecond) // the first token comes after half a second
	var decode float64
	for i := 0; i < 15; i++ {
		clock.advance(200 * time.Millisecond)
		if d, due := p.add(40); due { // 40 bytes at 4 bytes a token
			decode = d
		}
	}
	if !near(decode, 50, 6) {
		t.Errorf("decode speed read as %.1f tok/s, want about 50", decode)
	}
	if tokens, avg := p.average(); tokens != 150 || !near(avg, 50, 6) {
		t.Errorf("average %d tokens at %.1f tok/s, want 150 at about 50", tokens, avg)
	}
}

// the speed is the last second's: a stream that slows down says so
func TestPaceFollowsASlowdown(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	p := newPace(clock.now, 4)
	var decode float64
	for i := 0; i < 10; i++ { // 100 tok/s
		clock.advance(100 * time.Millisecond)
		if d, due := p.add(40); due {
			decode = d
		}
	}
	fast := decode
	for i := 0; i < 8; i++ { // 10 tok/s
		clock.advance(1 * time.Second)
		if d, due := p.add(40); due {
			decode = d
		}
	}
	if decode >= fast/3 {
		t.Errorf("after the stream slowed to 10 tok/s the reading is %.1f (was %.1f)", decode, fast)
	}
}

// one burst is not a speed, and the first reading never divides by a second that has not passed
func TestPaceSaysNothingFromASingleBurst(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	p := newPace(clock.now, 4)
	clock.advance(time.Second)
	if d, _ := p.add(400); d != 0 {
		t.Errorf("one chunk gave %.1f tok/s", d)
	}
}

// a server that reports its own speeds with every chunk needs no measuring
func TestPaceStandsDownWhenTheServerReportsItsOwn(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	p := newPace(clock.now, 4)
	p.exact = true
	clock.advance(time.Second)
	if _, due := p.add(400); due {
		t.Error("a measurement was reported over the server's own numbers")
	}
}

// reports are spaced, so the footer is not redrawn at the rate of the tokens
func TestPaceReportsAtMostEveryFewHundredMilliseconds(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1000, 0)}
	p := newPace(clock.now, 4)
	reports := 0
	for i := 0; i < 100; i++ { // a token every 10 ms for a second
		clock.advance(10 * time.Millisecond)
		if _, due := p.add(4); due {
			reports++
		}
	}
	if reports > 6 {
		t.Errorf("%d reports in a second, want about five", reports)
	}
}
