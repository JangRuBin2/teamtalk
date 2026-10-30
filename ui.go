package main

import (
	"fmt"
	"strings"
	"time"
)

const (
	cReset   = "\x1b[0m"
	cBold    = "\x1b[1m"
	cDim     = "\x1b[2m"
	cRed     = "\x1b[31m"
	cGreen   = "\x1b[32m"
	cYellow  = "\x1b[33m"
	cBlue    = "\x1b[34m"
	cMagenta = "\x1b[35m"
	cCyan    = "\x1b[36m"
)

var npcColors = []string{cCyan, cGreen, cYellow, cMagenta, cBlue, cRed}

func colorFor(idx int) string { return npcColors[idx%len(npcColors)] }

func clearScreen() { fmt.Print("\x1b[2J\x1b[H") }

func sysLine(format string, a ...any) {
	fmt.Printf(cDim+format+cReset+"\n", a...)
}

// printBubble prints a multi-line message aligned under the speaker label.
func printBubble(clock, color, who, text string) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	prefix := fmt.Sprintf("[%s] %s", clock, who)
	fmt.Printf("%s[%s]%s %s%s%s%s > %s\n", cDim, clock, cReset, color, cBold, who, cReset, lines[0])
	pad := strings.Repeat(" ", displayWidth(prefix)+3)
	for _, l := range lines[1:] {
		if l = strings.TrimSpace(l); l != "" {
			fmt.Printf("%s%s\n", pad, l)
		}
	}
}

// displayWidth approximates terminal width: Hangul and other wide runes take 2 columns.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115f || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) || (r >= 0xff00 && r <= 0xff60)) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

// spinner shows a "typing..." indicator until the returned func is called.
func spinner(label string) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		frames := []string{".  ", ".. ", "..."}
		for i := 0; ; i++ {
			fmt.Printf("\r%s%s%s%s", cDim, label, frames[i%len(frames)], cReset)
			select {
			case <-done:
				fmt.Print("\r\x1b[2K")
				return
			case <-time.After(400 * time.Millisecond):
			}
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

var fakeBuildLog = []string{
	"> backend@3.14.2 build",
	"> gradle clean build -x test --parallel",
	"",
	"Starting a Gradle Daemon (subsequent builds will be faster)",
	"> Task :common:compileJava UP-TO-DATE",
	"> Task :common:processResources UP-TO-DATE",
	"> Task :domain:compileJava",
	"> Task :domain:classes",
	"> Task :infra:compileJava",
	"Note: Some input files use unchecked or unsafe operations.",
	"Note: Recompile with -Xlint:unchecked for details.",
	"> Task :infra:classes",
	"> Task :api:compileJava",
	"> Task :api:processResources",
	"> Task :api:bootJar",
	"<===========--> 87% EXECUTING [1m 12s]",
	"> :api:bootJar > Resolve dependencies of :api:runtimeClasspath",
}

// bar renders a 0-100 value as a 10-cell gauge.
func bar(v int, color string) string {
	v = max(0, min(100, v))
	n := (v + 5) / 10
	return fmt.Sprintf("%s%s%s%s %3d", color, strings.Repeat("█", n), cDim+strings.Repeat("░", 10-n), cReset, v)
}

// bossKey hides the game behind a fake build log until Enter is pressed.
func bossKey(readLine func() (string, bool)) {
	clearScreen()
	for _, l := range fakeBuildLog {
		fmt.Println(l)
		time.Sleep(60 * time.Millisecond)
	}
	readLine()
	clearScreen()
}
