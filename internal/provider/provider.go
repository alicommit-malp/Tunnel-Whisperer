package provider

// Provider defines the interface for cloud infrastructure operations.
// Each cloud platform (AWS, GCP, Azure, etc.) implements this interface.
type Provider interface {
	// CreateInstance provisions a new relay instance.
	CreateInstance(domain string) error

	// ConfigureDNS sets up DNS records pointing the subdomain to the relay IP.
	ConfigureDNS(domain string, ip string) error

	// ConfigureFirewall opens the required ports (443, 80, 22) on the relay.
	ConfigureFirewall(instanceID string) error

	// Destroy tears down the relay instance and associated resources.
	Destroy(instanceID string) error
}
