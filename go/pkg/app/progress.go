package app

import (
	"fmt"
	"io"
	"strings"
	"time"
)

// FormatBytes writes a size the way hom 0.6.x did: "512 B", "1.5 KB", "3.2 MB", "1.25 GB".
func FormatBytes(b int64) string {
	if b < 0 {
		b = 0
	}
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	case b < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(b)/(1024*1024*1024))
}

// FormatProgress is one ASCII line of the progress bar: no '\r', no '\n'.
func FormatProgress(pct int, received, total int64, etaSec int) string {
	var s string
	if pct >= 0 && total > 0 {
		if pct > 100 {
			pct = 100
		}
		const width = 20
		filled := pct * width / 100
		s = "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
		s += fmt.Sprintf(" %3d%%  ", pct) + FormatBytes(received) + " / " + FormatBytes(total)
	} else {
		s = "downloading...  " + FormatBytes(received)
	}
	if etaSec >= 0 {
		if etaSec >= 60 {
			s += fmt.Sprintf("  ~%dm %02ds", etaSec/60, etaSec%60)
		} else {
			s += fmt.Sprintf("  ~%ds", etaSec)
		}
	}
	return s
}

// progress draws the download progress: a bar on a terminal, "@progress" lines for a program
// that reads hom's output (HomRec), nothing otherwise.
type progress struct {
	w       io.Writer
	mode    string
	start   time.Time
	last    time.Time
	emitted bool
	drew    bool
	lastLen int
}

func (e *Env) newProgress() *progress {
	mode := e.Progress
	if mode == "" {
		mode = "none"
		if e.OutTTY && !e.JSON {
			mode = "bar"
		}
	}
	if e.JSON && e.Progress == "" {
		mode = "none"
	}
	w := e.Stdout
	if e.JSON {
		w = e.Stderr // stdout holds the JSON document only
	}
	now := time.Now()
	return &progress{w: w, mode: mode, start: now, last: now}
}

// Update implements web.Progress.
func (p *progress) Update(received, total int64) {
	if p.mode == "none" {
		return
	}
	now := time.Now()
	final := total > 0 && received >= total
	if p.emitted && !final && now.Sub(p.last) < 100*time.Millisecond {
		return
	}
	p.last = now
	p.emitted = true

	elapsed := now.Sub(p.start).Seconds()
	pct := -1
	if total > 0 {
		pct = int(received * 100 / total)
		if pct > 100 {
			pct = 100
		}
	}
	eta := -1
	if !final && total > 0 && received > 0 && elapsed >= 0.5 {
		speed := float64(received) / elapsed
		eta = int(float64(total-received)/speed + 0.5)
	}
	if p.mode == "lines" {
		fmt.Fprintf(p.w, "@progress %d %d %d %d\n", pct, received, total, eta)
		return
	}
	line := "  " + FormatProgress(pct, received, total, eta)
	n := len(line)
	if n < p.lastLen {
		line += strings.Repeat(" ", p.lastLen-n) // wipe what is left of a longer previous line
	}
	p.lastLen = n
	fmt.Fprintf(p.w, "\r%s", line)
	p.drew = true
}

// Finish ends the redrawn line so the next output starts fresh.
func (p *progress) Finish() {
	if p.mode == "bar" && p.drew {
		fmt.Fprintln(p.w)
	}
	p.drew = false
}
