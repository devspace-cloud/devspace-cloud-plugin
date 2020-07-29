package loader

import (
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/devspace-cloud/devspace/pkg/devspace/config/versions"
	"github.com/devspace-cloud/devspace/pkg/devspace/config/versions/latest"
	"github.com/devspace-cloud/devspace/pkg/devspace/deploy/deployer/kubectl/walk"
	"github.com/devspace-cloud/devspace/pkg/util/survey"
	varspkg "github.com/devspace-cloud/devspace/pkg/util/vars"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
)

type variable struct {
	Definition *latest.Variable
	Value      interface{}
}

func varMatchFn(path, key, value string) bool {
	return varspkg.VarMatchRegex.MatchString(value)
}

// GetProfiles retrieves all available profiles
func (l *configLoader) GetProfiles() ([]string, error) {
	path := l.ConfigPath()
	bytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	rawMap := map[interface{}]interface{}{}
	err = yaml.Unmarshal(bytes, &rawMap)
	if err != nil {
		return nil, errors.Errorf("Error parsing devspace.yaml: %v", err)
	}

	profiles, ok := rawMap["profiles"].([]interface{})
	if !ok {
		profiles = []interface{}{}
	}

	profileNames := []string{}
	for _, profile := range profiles {
		profileMap, ok := profile.(map[interface{}]interface{})
		if !ok {
			continue
		}

		name, ok := profileMap["name"].(string)
		if !ok {
			continue
		}

		profileNames = append(profileNames, name)
	}

	return profileNames, nil
}

// ParseCommands fills the variables in the data and parses the commands
func (l *configLoader) ParseCommands() ([]*latest.CommandConfig, error) {
	data, err := l.LoadRaw()
	if err != nil {
		return nil, err
	}

	// Load defined variables
	vars, err := versions.ParseVariables(data, l.log)
	if err != nil {
		return nil, err
	}

	// Parse commands
	preparedConfig, err := versions.ParseCommands(data)
	if err != nil {
		return nil, err
	}

	// Fill in variables
	err = l.FillVariables(preparedConfig, vars)
	if err != nil {
		return nil, err
	}

	// Now parse the whole config
	parsedConfig, err := versions.Parse(preparedConfig, l.options.LoadedVars, l.log)
	if err != nil {
		return nil, errors.Wrap(err, "parse config")
	}

	return parsedConfig.Commands, nil
}

// parseConfig fills the variables in the data and parses the config
func (l *configLoader) parseConfig(data map[interface{}]interface{}) (*latest.Config, error) {
	// Get profile
	profiles, err := versions.ParseProfile(data, l.options.Profile)
	if err != nil {
		return nil, err
	}

	// Now delete not needed parts from config
	delete(data, "profiles")
	delete(data, "commands")

	// Apply profiles
	for i := len(profiles) - 1; i >= 0; i-- {
		// Apply replace
		err = ApplyReplace(data, profiles[i])
		if err != nil {
			return nil, err
		}

		// Apply patches
		data, err = ApplyPatches(data, profiles[i])
		if err != nil {
			return nil, err
		}
	}

	// Load defined variables
	vars, err := versions.ParseVariables(data, l.log)
	if err != nil {
		return nil, err
	}

	// Delete vars from config
	delete(data, "vars")

	// Fill in variables
	err = l.FillVariables(data, vars)
	if err != nil {
		return nil, err
	}

	// Now convert the whole config to latest
	latestConfig, err := versions.Parse(data, l.options.LoadedVars, l.log)
	if err != nil {
		return nil, errors.Wrap(err, "convert config")
	}

	return latestConfig, nil
}

// FillVariables fills in the given vars into the prepared config
func (l *configLoader) FillVariables(preparedConfig map[interface{}]interface{}, vars []*latest.Variable) error {
	// Find out what vars are really used
	varsUsed := map[string]bool{}
	err := walk.Walk(preparedConfig, varMatchFn, func(path, value string) (interface{}, error) {
		varspkg.ParseString(value, func(v string) (interface{}, error) {
			varsUsed[v] = true
			return "", nil
		})

		return value, nil
	})
	if err != nil {
		return err
	}

	// Parse cli --var's
	varsParsed, err := ParseVarsFromOptions(l.options)
	if err != nil {
		return err
	}

	// Fill used defined variables
	if len(vars) > 0 {
		newVars := []*latest.Variable{}
		for _, variable := range vars {
			if varsUsed[strings.TrimSpace(variable.Name)] {
				newVars = append(newVars, variable)
			}
		}

		if len(newVars) > 0 {
			err = l.askQuestions(newVars, varsParsed)
			if err != nil {
				return err
			}
		}
	}

	// Walk over data and fill in variables
	l.resolvedVars = map[string]string{}
	err = walk.Walk(preparedConfig, varMatchFn, func(path, value string) (interface{}, error) {
		return l.VarReplaceFn(path, value, varsParsed)
	})
	if err != nil {
		return err
	}

	return nil
}

// ParseVarsFromOptions returns the variables from the given options
func ParseVarsFromOptions(options *ConfigOptions) (map[string]*variable, error) {
	vars := map[string]*variable{}

	for _, cmdVar := range options.Vars {
		idx := strings.Index(cmdVar, "=")
		if idx == -1 {
			return nil, errors.Errorf("Wrong --var format: %s, expected 'key=val'", cmdVar)
		}

		vars[strings.TrimSpace(cmdVar[:idx])] = &variable{
			Value: strings.TrimSpace(cmdVar[idx+1:]),
		}
	}

	return vars, nil
}

func (l *configLoader) askQuestions(vars []*latest.Variable, varsParsed map[string]*variable) error {
	for _, definition := range vars {
		name := strings.TrimSpace(definition.Name)

		// check if var is already there
		if v, ok := varsParsed[name]; ok {
			v.Definition = definition
			continue
		}

		// fill the variable
		value, err := l.fillVariable(name, definition)
		if err != nil {
			return err
		}

		// set the variable value
		varsParsed[name] = &variable{
			Definition: definition,
			Value:      value,
		}
	}

	return nil
}

func (l *configLoader) VarReplaceFn(path, value string, vars map[string]*variable) (interface{}, error) {
	// Save old value
	if l.options.LoadedVars != nil {
		l.options.LoadedVars[path] = value
	}

	return varspkg.ParseString(value, func(v string) (interface{}, error) {
		val, err := l.ResolveVar(v, vars)
		if err != nil {
			return "", err
		}

		l.resolvedVars[v] = fmt.Sprintf("%v", val)
		return val, nil
	})
}

func (l *configLoader) ResolveVar(varName string, vars map[string]*variable) (interface{}, error) {
	// check if in vars already
	v, ok := vars[varName]
	if ok {
		return v.Value, nil
	}

	// fill the variable if not found
	value, err := l.fillVariable(varName, nil)
	if err != nil {
		return nil, err
	}

	// set variable so that we don't ask again
	vars[varName] = &variable{
		Value: value,
	}

	return value, nil
}

func (l *configLoader) fillVariable(varName string, definition *latest.Variable) (interface{}, error) {
	// is predefined variable?
	found, value, err := l.resolvePredefinedVar(varName)
	if err != nil {
		return "", err
	} else if found {
		return value, nil
	}

	// get the cache
	generatedConfig, err := l.Generated()
	if err != nil {
		return "", err
	}

	// fill variable without definition
	if definition == nil {
		// Is in environment?
		if os.Getenv(varName) != "" {
			return os.Getenv(varName), nil
		}

		// Is in generated config?
		if _, ok := generatedConfig.Vars[varName]; ok {
			return generatedConfig.Vars[varName], nil
		}

		// Ask for variable
		generatedConfig.Vars[varName], err = l.askQuestion(&latest.Variable{
			Question: "Please enter a value for " + varName,
		})
		if err != nil {
			return "", err
		}

		return generatedConfig.Vars[varName], nil
	}

	// fill variable by source
	switch definition.Source {
	case latest.VariableSourceEnv:
		// Check environment
		value := os.Getenv(varName)

		// Use default value for env variable if it is configured
		if value == "" {
			if definition.Default == nil {
				return nil, errors.Errorf("couldn't find environment variable %s, but is needed for loading the config", varName)
			}

			return definition.Default, nil
		}

		return value, nil
	case latest.VariableSourceDefault, latest.VariableSourceInput, latest.VariableSourceAll:
		// Check environment
		value := os.Getenv(varName)

		// Did we find it in the environment variables?
		if definition.Source != latest.VariableSourceInput && value != "" {
			return valueByType(value, definition)
		}

		// Is cached
		if value, ok := generatedConfig.Vars[varName]; ok {
			return valueByType(value, definition)
		}

		// Now ask the question
		value, err := l.askQuestion(definition)
		if err != nil {
			return nil, err
		}

		generatedConfig.Vars[varName] = value
		return valueByType(value, definition)
	case latest.VariableSourceNone:
		if definition.Default == nil {
			return nil, errors.Errorf("couldn't set variable '%s', because source is '%s' but the default value is empty", varName, latest.VariableSourceNone)
		}

		return definition.Default, nil
	default:
		return nil, errors.Errorf("unrecognized variable source '%s', please choose one of 'all', 'input', 'env' or 'none'", varName)
	}
}

func valueByType(value string, defaultValue interface{}) (interface{}, error) {
	if defaultValue == nil {
		return value, nil
	}

	switch defaultValue.(type) {
	case int:
		return strconv.Atoi(value)
	case bool:
		return strconv.ParseBool(value)
	default:
		return value, nil
	}
}

func (l *configLoader) askQuestion(variable *latest.Variable) (string, error) {
	params := &survey.QuestionOptions{}

	if variable == nil {
		params.Question = "Please enter a value"
	} else {
		if variable.Question == "" {
			if variable.Name == "" {
				variable.Name = "variable"
			}

			params.Question = "Please enter a value for " + variable.Name
		} else {
			params.Question = variable.Question
		}

		if variable.Password {
			params.IsPassword = true
		}

		if variable.Default != "" {
			params.DefaultValue = fmt.Sprintf("%v", variable.Default)
		}

		if len(variable.Options) > 0 {
			params.Options = variable.Options
			if variable.Default == nil {
				params.DefaultValue = params.Options[0]
			}
		} else if variable.ValidationPattern != "" {
			params.ValidationRegexPattern = variable.ValidationPattern

			if variable.ValidationMessage != "" {
				params.ValidationMessage = variable.ValidationMessage
			}
		}
	}

	answer, err := l.log.Question(params)
	if err != nil {
		return "", err
	}

	fmt.Println(answer)
	return answer, nil
}
