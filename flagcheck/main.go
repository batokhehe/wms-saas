package main

import (
	"flag"
	"fmt"
)

func main() {
	steps := flag.Int("steps", 1, "")
	env := flag.String("env", ".env", "")
	flag.CommandLine.Parse([]string{"-env", "", "down", "-steps", "3"})
	fmt.Printf("env=%q command=%q steps=%d args=%v\n", *env, flag.Arg(0), *steps, flag.Args())
}
