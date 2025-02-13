package main

import (
	"errors"
	"flag"
	"io/fs"
	"log"
)

var (
	flagTemplates = flag.String("templates", "", "templates to use for generating")
	flagOutput    = flag.String("out", "", "output directory for generated files")
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	flag.Parse()

	if flagTemplates == nil || *flagTemplates == "" {
		return errors.New("no templates specified")
	}
	if flagOutput == nil || *flagOutput == "" {
		return errors.New("no output directory specified")
	}

	return nil
}

type PlainFile struct {
	Name    string
	Path    string
	Content string
}

type TemplateSet struct {
	PlainFiles map[string]string
	FS         fs.FS
}
