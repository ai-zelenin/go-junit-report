package gojunitreport

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ai-zelenin/go-junit-report/v2/gtr"
	"github.com/ai-zelenin/go-junit-report/v2/junit"
	"github.com/ai-zelenin/go-junit-report/v2/parser/gotest"
)

type parser interface {
	Parse(r io.Reader) (gtr.Report, error)
	Events() []gotest.Event
}

// Config contains the go-junit-report command configuration.
type Config struct {
	Parser           string
	Hostname         string
	PackageName      string
	SkipXMLHeader    bool
	SubtestMode      gotest.SubtestMode
	Properties       map[string]string
	TimestampFunc    func() time.Time
	RequiredCoverage float64
	UnitConfigs      map[string]*gtr.UnitCfg
	// For debugging
	PrintEvents bool
}

// Run runs the go-junit-report command and returns the generated report.
func (c Config) Run(input io.Reader, output io.Writer) (*gtr.Report, error) {
	var p parser
	options := c.gotestOptions()
	unitCfg := c.UnitConfigs
	calcRequiredCover := func(packageName string) float64 {
		cfg := unitCfg[packageName]
		if cfg != nil {
			return cfg.MinCover
		}
		return c.RequiredCoverage
	}

	if c.RequiredCoverage > 0 {
		var reqCoverHandler = gotest.WithEventHandler(func(e gotest.Event) error {
			if e.Type == "summary" {
				rc := calcRequiredCover(e.Name)
				if e.CovPct < rc {
					fmt.Printf("COVERAGE FAIL: %s is to low %.1f < %.1f \n", e.Name, e.CovPct, rc)
				}
			}
			return nil
		})
		options = append(options, reqCoverHandler)
	}
	switch c.Parser {
	case "gotest":
		p = gotest.NewParser(options...)
	case "gojson":
		p = gotest.NewJSONParser(options...)
	default:
		return nil, fmt.Errorf("invalid parser: %s", c.Parser)
	}

	report, err := p.Parse(input)
	if err != nil {
		return nil, fmt.Errorf("error parsing input: %w", err)
	}

	if c.PrintEvents {
		enc := json.NewEncoder(os.Stderr)
		for _, event := range p.Events() {
			if err := enc.Encode(event); err != nil {
				return nil, err
			}
		}
	}

	for i := range report.Packages {
		for k, v := range c.Properties {
			report.Packages[i].SetProperty(k, v)
		}
	}
	if c.RequiredCoverage > 0 {
		for i, pp := range report.Packages {
			rc := calcRequiredCover(pp.Name)
			if pp.Coverage < rc && pp.RunError.Name == "" && pp.BuildError.Name == "" {
				desc := fmt.Sprintf("FAIL: %s %v %f < %f", pp.Name, gtr.ErrPackageCoverageIsTooLow, pp.Coverage, rc)
				name := gtr.ErrPackageCoverageIsTooLow.Error()
				report.Packages[i].RunError.Name = name
				report.Packages[i].RunError.Cause = "lowCoverage"
				report.Packages[i].RunError.Output = []string{desc}
			}
		}
	}

	if err = c.writeJunitXML(output, report); err != nil {
		return nil, err
	}
	return &report, nil
}

func (c Config) writeJunitXML(w io.Writer, report gtr.Report) error {
	testsuites := junit.CreateFromReport(report, c.Hostname)
	if !c.SkipXMLHeader {
		_, err := fmt.Fprintf(w, xml.Header)
		if err != nil {
			return err
		}
	}
	return testsuites.WriteXML(w)
}

func (c Config) gotestOptions() []gotest.Option {
	return []gotest.Option{
		gotest.PackageName(c.PackageName),
		gotest.SetSubtestMode(c.SubtestMode),
		gotest.TimestampFunc(c.TimestampFunc),
	}
}
