package agent

import "time"

const (
	paceWindow    = time.Second            // how far back the decode speed looks
	paceEvery     = 200 * time.Millisecond // how often it is reported: the footer needs no more
	paceMinTTFT   = 150 * time.Millisecond // a faster first token is the network, not the prompt being read
	paceMinPrompt = 64                     // fewer new prompt tokens than this say nothing about prefill speed
)

// pace measures the streaming speed from the client's side, for servers reporting only at the end
type pace struct {
	now      func() time.Time
	sent     time.Time // the request went out
	first    time.Time // the first token came back
	window   []paceTick
	total    float64 // tokens estimated so far
	reported time.Time
	rate     float64 // bytes per token
	exact    bool    // the server sends speeds with every chunk: nothing here is needed
}

type paceTick struct {
	at     time.Time
	tokens float64
}

// newPace starts the clock when a request goes out.
func newPace(now func() time.Time, bytesPerTok float64) *pace {
	return &pace{now: now, sent: now(), rate: max(bytesPerTok, minBytesPerTok)}
}

// add counts a reply chunk and says the speed over the last second, when there is one to read
func (p *pace) add(bytes int) (decode float64, due bool) {
	if p.exact || bytes <= 0 {
		return 0, false
	}
	at := p.now()
	tokens := float64(bytes) / p.rate
	p.total += tokens
	p.window = append(p.window, paceTick{at, tokens})
	if p.first.IsZero() {
		p.first = at
	}
	if !p.reported.IsZero() && at.Sub(p.reported) < paceEvery {
		return 0, false
	}
	if decode = p.decode(at); decode == 0 {
		return 0, false // one burst is not a speed
	}
	p.reported = at
	return decode, true
}

// decode is the tokens in the last window over the time they took, from the first token at the start
func (p *pace) decode(at time.Time) float64 {
	cut := at.Add(-paceWindow)
	i := 0
	for i < len(p.window) && !p.window[i].at.After(cut) { // the window is the last second, not a second and an instant
		i++
	}
	p.window = p.window[i:]
	if len(p.window) == 0 {
		return 0
	}
	var tokens float64
	for _, t := range p.window {
		tokens += t.tokens
	}
	span := at.Sub(p.first)
	if span > paceWindow {
		span = paceWindow
	}
	if span < 100*time.Millisecond {
		return 0 // one burst is not a speed
	}
	return tokens / span.Seconds()
}

// average is the speed over the whole reply, what a server silent about speed ends with
func (p *pace) average() (tokens int, perSecond float64) {
	if p.first.IsZero() {
		return 0, 0
	}
	took := p.now().Sub(p.first).Seconds()
	if took < 0.1 {
		return int(p.total), 0
	}
	return int(p.total), p.total / took
}
