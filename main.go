package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/jetstack/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/jetstack/cert-manager/pkg/acme/webhook/cmd"
	"github.com/jetstack/cert-manager/pkg/issuer/acme/dns/util"

	"github.com/namedotcom/go/namecom"
)

var GroupName = os.Getenv("GROUP_NAME")

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	// This will register our custom DNS provider with the webhook serving
	// library, making it available as an API under the provided GroupName.
	// You can register multiple DNS provider implementations with a single
	// webhook, where the Name() method will be used to disambiguate between
	// the different implementations.
	cmd.RunWebhookServer(GroupName,
		&namedotcomDNSProviderSolver{},
	)
}

// namedotcomDNSProviderSolver implements the provider-specific logic needed to
// 'present' an ACME challenge TXT record for your own DNS provider.
// To do so, it must implement the `github.com/jetstack/cert-manager/pkg/acme/webhook.Solver`
// interface.
type namedotcomDNSProviderSolver struct {
	// If a Kubernetes 'clientset' is needed, you must:
	// 1. uncomment the additional `client` field in this structure below
	// 2. uncomment the "k8s.io/client-go/kubernetes" import at the top of the file
	// 3. uncomment the relevant code in the Initialize method below
	// 4. ensure your webhook's service account has the required RBAC role
	//    assigned to it for interacting with the Kubernetes APIs you need.
	client   *kubernetes.Clientset
	recordId int32
}

// namedotcomDNSProviderConfig is a structure that is used to decode into when
// solving a DNS01 challenge.
// This information is provided by cert-manager, and may be a reference to
// additional configuration that's needed to solve the challenge for this
// particular certificate or issuer.
// This typically includes references to Secret resources containing DNS
// provider credentials, in cases where a 'multi-tenant' DNS solver is being
// created.
// If you do *not* require per-issuer or per-certificate configuration to be
// provided to your webhook, you can skip decoding altogether in favour of
// using CLI flags or similar to provide configuration.
// You should not include sensitive information here. If credentials need to
// be used by your provider here, you should reference a Kubernetes Secret
// resource and fetch these credentials using a Kubernetes clientset.
type namedotcomDNSProviderConfig struct {
	// Change the two fields below according to the format of the configuration
	// to be decoded.
	// These fields will be set by users in the
	// `issuer.spec.acme.dns01.providers.webhook.config` field.
	Username          string                   `json:"username"`
	ApiTokenSecretRef corev1.SecretKeySelector `json:"apitokensecret"`
}

// Name is used as the name for this DNS solver when referencing it on the ACME
// Issuer resource.
// This should be unique **within the group name**, i.e. you can have two
// solvers configured with the same Name() **so long as they do not co-exist
// within a single webhook deployment**.
// For example, `cloudflare` may be used as the name of a solver.
func (c *namedotcomDNSProviderSolver) Name() string {
	return "namedotcom"
}

// Present is responsible for actually presenting the DNS record with the
// DNS provider.
// This method should tolerate being called multiple times with the same value.
// cert-manager itself will later perform a self check to ensure that the
// solver has correctly configured the DNS provider.
func (c *namedotcomDNSProviderSolver) Present(ch *v1alpha1.ChallengeRequest) error {
	domainName := extractDomainName(ch.ResolvedZone)
	recordName := extractRecordName(ch.ResolvedFQDN, ch.ResolvedZone)

	nc, err := c.namedotcomAPIClient(ch)
	if err != nil {
		return err
	}

	fmt.Printf("Presenting record for %s (%s, %s)\n", ch.ResolvedFQDN, recordName, domainName)

	response, err := nc.CreateRecord(&namecom.Record{
		DomainName: domainName,
		Host:       recordName,
		Type:       "TXT",
		Answer:     ch.Key,
		TTL:        300,
	})
	if err != nil {
		fmt.Printf("Error: %+v\n", err)
		return err
	}
	c.recordId = response.ID
	return nil
}

// CleanUp should delete the relevant TXT record from the DNS provider console.
// If multiple TXT records exist with the same record name (e.g.
// _acme-challenge.example.com) then **only** the record with the same `key`
// value provided on the ChallengeRequest should be cleaned up.
// This is in order to facilitate multiple DNS validations for the same domain
// concurrently.
func (c *namedotcomDNSProviderSolver) CleanUp(ch *v1alpha1.ChallengeRequest) error {
	domainName := extractDomainName(ch.ResolvedZone)

	nc, err := c.namedotcomAPIClient(ch)
	if err != nil {
		return err
	}

	fmt.Printf("Cleaning up record for %s (%s)", ch.ResolvedFQDN, domainName)

	empty, err := nc.DeleteRecord(&namecom.DeleteRecordRequest{
		DomainName: domainName,
		ID:         c.recordId,
	})
	if err != nil {
		return err
	}
	_ = empty
	return nil
}

// Initialize will be called when the webhook first starts.
// This method can be used to instantiate the webhook, i.e. initialising
// connections or warming up caches.
// Typically, the kubeClientConfig parameter is used to build a Kubernetes
// client that can be used to fetch resources from the Kubernetes API, e.g.
// Secret resources containing credentials used to authenticate with DNS
// provider accounts.
// The stopCh can be used to handle early termination of the webhook, in cases
// where a SIGTERM or similar signal is sent to the webhook process.
func (c *namedotcomDNSProviderSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}

	c.client = cl

	return nil
}

// Create a name.com API client using a secret token
func (c *namedotcomDNSProviderSolver) namedotcomAPIClient(ch *v1alpha1.ChallengeRequest) (*namecom.NameCom, error) {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return nil, err
	}

	err = c.validate(&cfg, ch.AllowAmbientCredentials)
	if err != nil {
		return nil, err
	}

	apiToken, err := c.secret(cfg.ApiTokenSecretRef, ch.ResourceNamespace)
	if err != nil {
		return nil, err
	}
	nc := namecom.New(cfg.Username, apiToken)
	return nc, nil
}

// Validate config
func (c *namedotcomDNSProviderSolver) validate(cfg *namedotcomDNSProviderConfig, allowAmbientCredentials bool) error {
	if allowAmbientCredentials {
		// When allowAmbientCredentials is true, OVH client can load missing config
		// values from the environment variables and the ovh.conf files.
		return nil
	}
	if cfg.Username == "" {
		return errors.New("No Name.com username provided in config")
	}
	if cfg.ApiTokenSecretRef.Name == "" {
		return errors.New("No Name.com API token secret provided in config")
	}
	return nil
}

// Fetch the API token from secrets
func (c *namedotcomDNSProviderSolver) secret(ref corev1.SecretKeySelector, namespace string) (string, error) {
	if ref.Name == "" {
		return "", nil
	}

	secret, err := c.client.CoreV1().Secrets(namespace).Get(context.TODO(), ref.Name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	apiToken := secret.Data[ref.Key]
	return string(apiToken), nil
}

// loadConfig is a small helper function that decodes JSON configuration into
// the typed config struct.
func loadConfig(cfgJSON *extapi.JSON) (namedotcomDNSProviderConfig, error) {
	cfg := namedotcomDNSProviderConfig{}
	// handle the 'base case' where no configuration has been provided
	if cfgJSON == nil {
		return cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("error decoding solver config: %v", err)
	}

	return cfg, nil
}

func extractRecordName(fqdn, domain string) string {
	name := util.UnFqdn(fqdn)
	if idx := strings.Index(name, "."+util.UnFqdn(domain)); idx != -1 {
		return name[:idx]
	}
	return name
}

func extractDomainName(zone string) string {
	authZone, err := util.FindZoneByFqdn(zone, util.RecursiveNameservers)
	if err != nil {
		fmt.Printf("could not get zone by fqdn %v", err)
		return zone
	}
	return util.UnFqdn(authZone)
}
