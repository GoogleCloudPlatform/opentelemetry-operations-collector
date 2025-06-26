package command

import (
	"flag"
	"strings"
)

type ArrayFlag []string

func NewArrayFlag(flagSet *flag.FlagSet, name, usage string) *ArrayFlag {
	var newFlagValue ArrayFlag
	flagSet.Var(&newFlagValue, name, usage)
	return &newFlagValue
}

// Implements flag.Value
func (a *ArrayFlag) String() string {
	return strings.Join(*a, " ")
}

func (a *ArrayFlag) Set(value string) error {
	values := []string{value}
	if strings.Contains(value, ",") {
		values = strings.Split(value, ",")
	}
	*a = append(*a, values...)
	return nil
}
