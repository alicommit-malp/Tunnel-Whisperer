package aws

import "fmt"

var ErrNotImplemented = fmt.Errorf("aws provider: not implemented")

// Provider implements the cloud provider interface for AWS.
type Provider struct {
	Region string
}

func New(region string) *Provider {
	return &Provider{Region: region}
}

func (p *Provider) CreateInstance(domain string) error {
	return fmt.Errorf("CreateInstance(%s): %w", domain, ErrNotImplemented)
}

func (p *Provider) ConfigureDNS(domain string, ip string) error {
	return fmt.Errorf("ConfigureDNS(%s, %s): %w", domain, ip, ErrNotImplemented)
}

func (p *Provider) ConfigureFirewall(instanceID string) error {
	return fmt.Errorf("ConfigureFirewall(%s): %w", instanceID, ErrNotImplemented)
}

func (p *Provider) Destroy(instanceID string) error {
	return fmt.Errorf("Destroy(%s): %w", instanceID, ErrNotImplemented)
}
