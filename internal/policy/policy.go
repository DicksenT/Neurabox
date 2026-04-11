package policy

import (
	"os"
	"gopkg.in/yaml.v3"
)

type Mount struct{
	Source	string `yaml:"source"`
	Target	string `yaml:"target"`
	Mode 	string `yaml:"mode" `
}

type CheckType struct{
	CName	string `yaml:"cname"` 
	Command	string `yaml:"command"`
}

type Config struct{
	Name	string `yaml:"name"`
	Image	string `yaml:"image"`
	Mounts []Mount `yaml:"mounts"`
	Blocks []string `yaml:"blocks"`
	Checks []CheckType `yaml:"checks"`
}

func Load(path string) (*Config, error){
	f, err := os.ReadFile(path)
	if(err!=nil) {return nil, err}
	var cfg Config
	err = yaml.Unmarshal(f, &cfg)
	return &cfg, err
}