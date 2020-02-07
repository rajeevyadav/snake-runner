package main

import (
	"errors"

	"github.com/reconquest/karma-go"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Variables map[string]string    `json:"variables" yaml:"variables"`
	Shell     string               `json:"shell"     yaml:"shell"`
	Image     string               `json:"image"     yaml:"image"`
	Stages    []string             `json:"stages"    yaml:"stages"`
	Jobs      map[string]ConfigJob `json:"jobs"      yaml:"jobs"`
}

type ConfigJob struct {
	Variables map[string]string `json:"variables" yaml:"variables"`
	Stage     string            `yaml:"stage"     yaml:"stage"`
	Shell     string            `yaml:"shell"     yaml:"shell"`
	Image     string            `yaml:"image"     yaml:"image"`
	Commands  []string          `yaml:"commands"  yaml:"commands"`
}

func unmarshalConfig(data []byte) (Config, error) {
	var config Config

	raw := map[string]yaml.Node{}
	err := yaml.Unmarshal(data, &raw)
	if err != nil {
		return config, err
	}

	if node, ok := raw["image"]; !ok {
		return config, errors.New("missing image field")
	} else {
		err = node.Decode(&config.Image)
		if err != nil {
			return config, karma.Format(
				err,
				"invalid yaml field: 'image'",
			)
		}

		delete(raw, "image")
	}

	if node, ok := raw["stages"]; !ok {
		return config, errors.New("missing stages field")
	} else {
		err = node.Decode(&config.Stages)
		if err != nil {
			return config, karma.Format(
				err,
				"invalid yaml field: 'stages'",
			)
		}

		delete(raw, "stages")
	}

	config.Jobs = map[string]ConfigJob{}
	for jobName, node := range raw {
		var job ConfigJob
		err := node.Decode(&job)
		if err != nil {
			return config, karma.Format(
				err,
				"invalid yaml job: '%s'", jobName,
			)
		}

		config.Jobs[jobName] = job
	}

	if node, ok := raw["variables"]; ok {
		err = node.Decode(&config.Variables)
		if err != nil {
			return config, karma.Format(
				err,
				"invalid yaml field: 'variables'",
			)
		}
	}

	return config, nil
}
