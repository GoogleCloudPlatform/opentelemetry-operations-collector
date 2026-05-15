package fluentbit

type Component struct {
	Kind          string
	Config        map[string]string
	OrderedConfig [][2]string
}

type ModularConfig struct {
	Variables  map[string]string
	Components []Component
}

func (c ModularConfig) Generate() (map[string]string, error) {
	return nil, nil
}

const MetricsPort = 20202

func MetricsInputComponent() Component {
	return Component{}
}

func MetricsOutputComponent(port int) Component {
	return Component{}
}

const (
	outputFileKind     = "OPSAGENTOUTPUTFILE"
	outputFileName     = "filename"
	outputFileContents = "contents"
)

func outputFileComponent(name, contents string) Component {
	return Component{
		Kind: outputFileKind,
		Config: map[string]string{
			outputFileName:     name,
			outputFileContents: contents,
		},
	}
}
