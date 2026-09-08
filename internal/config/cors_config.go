package config

import "gopkg.in/yaml.v3"

type CORSConfig struct {
	AllowOrigin      string   `yaml:"allow_origin"`      // Access-Control-Allow-Origin (default: "*")
	AllowMethods     []string `yaml:"allow_methods"`     // Access-Control-Allow-Methods
	AllowHeaders     []string `yaml:"allow_headers"`     // Access-Control-Allow-Headers
	ExposeHeaders    []string `yaml:"expose_headers"`    // Access-Control-Expose-Headers
	MaxAge           int      `yaml:"max_age"`           // Access-Control-Max-Age in seconds (default: 1728000)
	AllowCredentials bool     `yaml:"allow_credentials"` // Access-Control-Allow-Credentials (default: false)

	present CORSFieldPresence `yaml:"-"`
}

type CORSFieldPresence struct {
	AllowOrigin      bool
	AllowMethods     bool
	AllowHeaders     bool
	ExposeHeaders    bool
	MaxAge           bool
	AllowCredentials bool
}

func (c CORSConfig) Presence() CORSFieldPresence { return c.present }

func (c *CORSConfig) SetPresence(p CORSFieldPresence) { c.present = p }

func (c *CORSConfig) UnmarshalYAML(value *yaml.Node) error {
	type plain struct {
		AllowOrigin      string   `yaml:"allow_origin"`
		AllowMethods     []string `yaml:"allow_methods"`
		AllowHeaders     []string `yaml:"allow_headers"`
		ExposeHeaders    []string `yaml:"expose_headers"`
		MaxAge           int      `yaml:"max_age"`
		AllowCredentials bool     `yaml:"allow_credentials"`
	}
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	c.AllowOrigin = p.AllowOrigin
	c.AllowMethods = p.AllowMethods
	c.AllowHeaders = p.AllowHeaders
	c.ExposeHeaders = p.ExposeHeaders
	c.MaxAge = p.MaxAge
	c.AllowCredentials = p.AllowCredentials

	// A null value still produces a present key in the mapping node, so both
	// `allow_origin:` (null) and `allow_origin: ""` mark the field as set.
	if value.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(value.Content); i += 2 {
			switch value.Content[i].Value {
			case "allow_origin":
				c.present.AllowOrigin = true
			case "allow_methods":
				c.present.AllowMethods = true
			case "allow_headers":
				c.present.AllowHeaders = true
			case "expose_headers":
				c.present.ExposeHeaders = true
			case "max_age":
				c.present.MaxAge = true
			case "allow_credentials":
				c.present.AllowCredentials = true
			}
		}
	}
	return nil
}
