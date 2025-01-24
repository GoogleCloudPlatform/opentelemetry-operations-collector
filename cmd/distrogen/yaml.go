package main

import (
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

func yamlUnmarshalFromFile[T any](path string) (*T, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var result T
	err = yaml.Unmarshal(content, &result)
	if err != nil {
		return nil, wrapYamlErr(err, path)
	}
	return &result, wrapYamlErr(err, path)
}

func yamlMarshalToFile[T any](value *T, path string) error {
	content, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, fs.ModePerm)
}

func mapMerge[K comparable, V any](m map[K]V, m2 map[K]V) {
	if m == nil || m2 == nil {
		return
	}
	for k, v := range m2 {
		m[k] = v
	}
}

func mapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func wrapYamlErr(err error, path string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("error parsing %s: %w", path, err)
}

func renderYaml(value any) string {
	content, _ := yaml.Marshal(value)
	return string(content)
}
