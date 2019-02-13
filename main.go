package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl2/gohcl"
	"github.com/hashicorp/hcl2/hcl"
	"github.com/hashicorp/hcl2/hclparse"
	"github.com/zclconf/go-cty/cty"
)

type Variable struct {
	Name    string         `hcl:"name,label"`
	Default hcl.Attributes `hcl:"default,remain"`
}

type ClusterConfig struct {
	ControllerCount int `hcl:"controller_count,attr"`
	WorkerCount     int `hcl:"worker_count,attr"`
}

type Cluster struct {
	Name          string   `hcl:"name,label"`
	ClusterConfig hcl.Body `hcl:",remain"`
}

type FooComponentConfig struct {
	Foo *string `hcl:"foo,attr"`
}

func (foo *FooComponentConfig) PrintAttrs() {
	fmt.Printf("Foo: %s\n", *foo.Foo)
}

type BarComponentConfig struct {
	Bar string `hcl:"bar,attr"`
}

func (bar *BarComponentConfig) PrintAttrs() {
	fmt.Printf("Bar: %s\n", bar.Bar)
}

type Component struct {
	Type   string   `hcl:"type,label"`
	Config hcl.Body `hcl:",remain"`
}

type ComponentInterface interface {
	PrintAttrs()
}

var components = map[string]ComponentInterface{
	"foo": &FooComponentConfig{},
	"bar": &BarComponentConfig{},
}

type ConfigRoot struct {
	Cluster    Cluster     `hcl:"cluster,block"`
	Components []Component `hcl:"component,block"`
	Variables  []Variable  `hcl:"variable,block"`
}

func main() {
	configFiles, err := filepath.Glob("./*.datcfg")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("config files: %+v\n", configFiles)

	hclParser := hclparse.NewParser()

	var hclFiles []*hcl.File
	for _, f := range configFiles {
		hclFile, diags := hclParser.ParseHCLFile(f)

		exitIfDiags(diags)

		hclFiles = append(hclFiles, hclFile)
	}

	configBody := hcl.MergeFiles(hclFiles)

	userVals, diags := LoadValuesFile("dat.vars")

	exitIfDiags(diags)

	fmt.Printf("user values: %+v\n", userVals)

	var configRoot ConfigRoot
	diags = gohcl.DecodeBody(configBody, nil, &configRoot)

	exitIfDiags(diags)

	fmt.Printf("config root: %+v\n", configRoot)

	variables := map[string]cty.Value{}
	for _, v := range configRoot.Variables {
		if len(v.Default) == 0 {
			continue
		}

		defaultVal, diags := v.Default["default"].Expr.Value(nil)

		exitIfDiags(diags)

		if userVal, ok := userVals[v.Name]; ok {
			variables[v.Name] = userVal
		} else {
			variables[v.Name] = defaultVal
		}
	}

	evalContext := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": cty.ObjectVal(variables),
		},
	}

	var clusterConfig ClusterConfig
	diags = gohcl.DecodeBody(configRoot.Cluster.ClusterConfig, evalContext, &clusterConfig)

	exitIfDiags(diags)

	fmt.Printf("config cluster: %+v\n", clusterConfig)

	for _, componentConfig := range configRoot.Components {
		component, ok := components[componentConfig.Type]
		if !ok {
			fmt.Fprintf(os.Stderr, "Unknown component kind: %s\n", componentConfig.Type)
			os.Exit(1)
		}

		diags = gohcl.DecodeBody(componentConfig.Config, evalContext, component)

		exitIfDiags(diags)

		fmt.Printf("component config for %q: %+v\n", componentConfig.Type, component)

		component.PrintAttrs()
	}
}

// LoadValuesFile reads the file at the given path and parses it as a
// "values file" (flat key.value HCL config) for later use in the
// `EvalContext`.
//
// Adapted from
// https://github.com/hashicorp/terraform/blob/d4ac68423c4998279f33404db46809d27a5c2362/configs/parser_values.go#L8-L23
func LoadValuesFile(path string) (map[string]cty.Value, hcl.Diagnostics) {
	hclParser := hclparse.NewParser()
	varsFile, diags := hclParser.ParseHCLFile(path)
	if diags != nil {
		return nil, diags
	}

	body := varsFile.Body
	if body == nil {
		return nil, diags
	}

	vars := make(map[string]cty.Value)
	attrs, attrsDiags := body.JustAttributes()
	diags = append(diags, attrsDiags...)
	if attrs == nil {
		return vars, diags
	}

	for name, attr := range attrs {
		val, valDiags := attr.Expr.Value(nil)
		diags = append(diags, valDiags...)
		vars[name] = val
	}

	return vars, diags
}

func exitIfDiags(diags hcl.Diagnostics) {
	if len(diags) == 0 {
		return
	}
	for _, diag := range diags {
		fmt.Fprintf(os.Stderr, "%v\n", diag)
	}
	os.Exit(1)
}
