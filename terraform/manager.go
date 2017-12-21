package terraform

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/cloudfoundry/bosh-bootloader/storage"
	"github.com/coreos/go-semver/semver"
)

type Manager struct {
	executor              executor
	templateGenerator     TemplateGenerator
	inputGenerator        InputGenerator
	terraformOutputBuffer *bytes.Buffer
	logger                logger
}

type executor interface {
	Version() (string, error)
	Destroy() error
	Init(terraformTemplate string, inputs map[string]interface{}) error
	Apply(inputs map[string]interface{}) error
	Outputs() (map[string]interface{}, error)
	Output(string) (string, error)
}

type InputGenerator interface {
	Generate(storage.State) (map[string]interface{}, error)
}

type TemplateGenerator interface {
	Generate(storage.State) string
}

type logger interface {
	Step(string, ...interface{})
}

func NewManager(executor executor, templateGenerator TemplateGenerator, inputGenerator InputGenerator, terraformOutputBuffer *bytes.Buffer, logger logger) Manager {
	return Manager{
		executor:              executor,
		templateGenerator:     templateGenerator,
		inputGenerator:        inputGenerator,
		terraformOutputBuffer: terraformOutputBuffer,
		logger:                logger,
	}
}

func (m Manager) Version() (string, error) {
	return m.executor.Version()
}

func (m Manager) ValidateVersion() error {
	version, err := m.executor.Version()
	if err != nil {
		return err
	}

	currentVersion, err := semver.NewVersion(version)
	if err != nil {
		return err
	}

	minimumVersion, err := semver.NewVersion("0.10.0")
	if err != nil {
		return err
	}

	if currentVersion.LessThan(*minimumVersion) {
		return errors.New("Terraform version must be at least v0.10.0")
	}

	return nil
}

func (m Manager) Init(bblState storage.State) error {
	m.logger.Step("generating terraform template")
	template := m.templateGenerator.Generate(bblState)

	m.logger.Step("generating terraform inputs")
	input, err := m.inputGenerator.Generate(bblState)
	if err != nil {
		return fmt.Errorf("Input generator generate: %s", err)
	}

	err = m.executor.Init(template, input)
	if err != nil {
		return fmt.Errorf("Executor init: %s", err)
	}

	return nil
}

func (m Manager) Apply(bblState storage.State) (storage.State, error) {
	m.logger.Step("terraform apply")
	//Create the tmp service account key path
	inputs := map[string]string{}
	if bblState.IAAS == "gcp" {
		inputs["credentials"] = state.GCP.ServiceAccountKeyPath
	}
	err := m.executor.Apply(inputs)

	bblState.LatestTFOutput = readAndReset(m.terraformOutputBuffer)

	if err != nil {
		return bblState, fmt.Errorf("Executor apply: %s", err)
	}

	return bblState, nil
}

func (m Manager) Destroy(bblState storage.State) (storage.State, error) {
	m.logger.Step("destroying infrastructure")
	m.logger.Step("generating terraform inputs")
	_, err := m.inputGenerator.Generate(bblState)
	if err != nil {
		return storage.State{}, fmt.Errorf("Input generator generate: %s", err)
	}

	m.logger.Step("terraform destroy")
	err = m.executor.Destroy()

	bblState.LatestTFOutput = readAndReset(m.terraformOutputBuffer)

	if err != nil {
		return bblState, fmt.Errorf("Executor destroy: %s", err)
	}
	m.logger.Step("finished destroying infrastructure")

	return bblState, nil
}

func (m Manager) GetOutputs() (Outputs, error) {
	tfOutputs, err := m.executor.Outputs()
	if err != nil {
		return Outputs{}, err
	}

	return Outputs{Map: tfOutputs}, nil
}

func readAndReset(buf *bytes.Buffer) string {
	contents := buf.Bytes()
	buf.Reset()

	return string(contents)
}
