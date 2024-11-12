package main

import (
	"errors"
	"flag"
	"strings"
)

var (
	flagSpec       = flag.String("spec", "", "The distribution specification to use")
	flagForce      = flag.Bool("force", false, "Force generate even if there are no differences detected")
	flagVerbose    = flag.Bool("v", false, "Verbose output")
	flagRegistry   = newArrayFlag("registry", "Provide additional component registries")
	flagOtelConfig = flag.String("otel_config", "", "An OTel Config to generate a spec from configured components")

	errNoSpecFlag = errors.New("missing --spec flag")
)

type arrayFlag []string

func newArrayFlag(name, usage string) *arrayFlag {
	var newFlagValue arrayFlag
	flag.Var(&newFlagValue, name, usage)
	return &newFlagValue
}

// Implements flag.Value
func (a *arrayFlag) String() string {
	return strings.Join(*a, " ")
}

func (a *arrayFlag) Set(value string) error {
	values := []string{value}
	if strings.Contains(value, ",") {
		values = strings.Split(value, ",")
	}
	*a = append(*a, values...)
	return nil
}
