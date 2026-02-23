package ops

import (
	"fmt"
	"net/http"
	"time"
)

// CloudProvider describes one supported cloud provider.
type CloudProvider struct {
	Name      string `json:"name"`
	Key       string `json:"key"`        // matches terraform.Config.Provider
	TokenName string `json:"token_name"` // display label for the credential
	TokenLink string `json:"token_link"` // URL where the user creates the token
	VarName   string `json:"var_name"`   // Terraform variable name (empty for AWS)
}

// CloudProviders returns the list of supported cloud providers.
func CloudProviders() []CloudProvider {
	return []CloudProvider{
		{
			Name:      "Hetzner",
			Key:       "hetzner",
			TokenName: "API Token",
			TokenLink: "https://console.hetzner.cloud → Project → Security → API Tokens → Generate",
			VarName:   "hcloud_token",
		},
		{
			Name:      "DigitalOcean",
			Key:       "digitalocean",
			TokenName: "API Token",
			TokenLink: "https://cloud.digitalocean.com/account/api/tokens → Generate New Token",
			VarName:   "do_token",
		},
		{
			Name:      "AWS",
			Key:       "aws",
			TokenName: "Access Key",
			TokenLink: "https://console.aws.amazon.com/iam/ → Users → Security Credentials → Create Access Key",
		},
	}
}

// TestCloudCredentials validates credentials for the given provider.
func (o *Ops) TestCloudCredentials(providerName, token, awsSecret string) error {
	switch providerName {
	case "Hetzner":
		return testHTTPToken("https://api.hetzner.cloud/v1/servers", token)
	case "DigitalOcean":
		return testHTTPToken("https://api.digitalocean.com/v2/account", token)
	case "AWS":
		if len(token) < 16 {
			return fmt.Errorf("Access Key ID looks too short")
		}
		if len(awsSecret) < 30 {
			return fmt.Errorf("Secret Access Key looks too short")
		}
		return nil
	}
	return fmt.Errorf("unknown provider: %s", providerName)
}

func testHTTPToken(url, token string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return fmt.Errorf("invalid token (HTTP 401)")
	}
	return nil
}
