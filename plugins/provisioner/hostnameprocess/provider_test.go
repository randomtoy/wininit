package hostnameprocess

import (
	"context"
	"errors"
	provisionerv1 "github.com/randomtoy/wininit/sdk/provisioner/v1"
	"testing"
)

type platform struct {
	active, configured string
	sets               int
	fail               bool
}

func (p *platform) Snapshot(context.Context) (string, string, error) {
	return p.active, p.configured, nil
}
func (p *platform) Set(ctx context.Context, value string) error {
	if p.fail {
		return errors.New("platform refused mutation")
	}
	p.sets++
	p.configured = value
	return nil
}
func TestProviderRerunAndRestartOutcomes(t *testing.T) {
	p := &platform{active: "old", configured: "old"}
	provider := Provider{Platform: p}
	request := &provisionerv1.ApplyRequest{Metadata: &provisionerv1.Metadata{InstanceId: "instance", LocalHostname: "new-name", Datasource: "nocloud"}}
	for _, outcome := range []provisionerv1.Outcome{provisionerv1.Outcome_CHANGED_REBOOT_REQUIRED, provisionerv1.Outcome_REBOOT_REQUIRED} {
		value, err := provider.Apply(context.Background(), request)
		if err != nil || value.GetOutcome() != outcome {
			t.Fatalf("outcome=%v err=%v", value, err)
		}
	}
	p.active = p.configured
	value, err := provider.Apply(context.Background(), request)
	if err != nil || value.GetOutcome() != provisionerv1.Outcome_UNCHANGED || p.sets != 1 {
		t.Fatalf("replayed mutation: %v %v sets=%d", value, err, p.sets)
	}
	request.Metadata.LocalHostname = ""
	value, err = provider.Apply(context.Background(), request)
	if err != nil || value.GetOutcome() != provisionerv1.Outcome_SKIPPED {
		t.Fatal(value, err)
	}
}
func TestProviderRejectsInvalidHostnameBeforeMutation(t *testing.T) {
	for _, name := range []string{"bad/path", "1234", "longer-than-fifteen"} {
		p := &platform{}
		_, err := (Provider{Platform: p}).Apply(context.Background(), &provisionerv1.ApplyRequest{Metadata: &provisionerv1.Metadata{InstanceId: "instance", LocalHostname: name, Datasource: "configdrive"}})
		if err == nil || p.sets != 0 {
			t.Fatal("invalid hostname reached mutation")
		}
	}
}
func TestProviderPropagatesPlatformFailureAndCancellation(t *testing.T) {
	p := &platform{active: "old", configured: "old", fail: true}
	request := &provisionerv1.ApplyRequest{Metadata: &provisionerv1.Metadata{InstanceId: "instance", LocalHostname: "new-name", Datasource: "nocloud"}}
	if _, err := (Provider{Platform: p}).Apply(context.Background(), request); err == nil {
		t.Fatal("platform error lost")
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if _, err := (Provider{Platform: p}).Apply(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
