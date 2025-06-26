package command

import (
	"errors"
	"fmt"
	"os"
)

var errCommandNotFound = errors.New("command not found")

type Command interface {
	Run() error
	ParseArgs(args []string) error
}

type Runner struct {
	commands map[string]Command
}

func NewRunner() *Runner {
	return &Runner{
		commands: make(map[string]Command),
	}
}

func (cs *Runner) Register(name string, cmd Command) {
	cs.commands[name] = cmd
}

func (cs *Runner) Run(name string) error {
	cmd, ok := cs.commands[name]
	if !ok {
		return fmt.Errorf("%w: %s", errCommandNotFound, name)
	}
	args := []string{}
	if len(os.Args) > 2 {
		args = os.Args[2:]
	}
	err := cmd.ParseArgs(args)
	if err != nil {
		return err
	}
	return cmd.Run()
}
