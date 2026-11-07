// Command quietmatch prints the stdin lines that are a quiet phrase and exits 1 on any hit (-any: phrase anywhere in the line).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"

	"github.com/kercre123/wire-pod/chipper/pkg/myvector/quiet"
)

func main() {
	anywhere := flag.Bool("any", false, "match a phrase anywhere in the line, not the whole line")
	flag.Parse()
	match := quiet.Match
	if *anywhere {
		match = quiet.MatchAny
	}
	hit := false
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if _, ok := match(sc.Text()); ok {
			fmt.Println(sc.Text())
			hit = true
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "quietmatch:", err)
		os.Exit(2)
	}
	if hit {
		os.Exit(1)
	}
}
