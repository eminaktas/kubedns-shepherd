package v1alpha1

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

func TestDNSClass(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DNSClass Suite")
}

var _ = Describe("DNSClass", func() {
	Describe("ExtractTemplateKeysRegex", func() {
		It("returns unique keys discovered in DNSConfig searches in order", func() {
			dnsClass := &DNSClass{
				Spec: DNSClassSpec{
					DNSConfig: &corev1.PodDNSConfig{
						Searches: []string{
							"svc.{{ .clusterDomain }}",
							"{{ .namespace }}.svc.{{ .clusterDomain }}",
							"svc.{{ .env }}.{{ .namespace }}",
							"{{ .clusterDomain }}",
							"no templating here",
						},
					},
				},
			}

			Expect(dnsClass.ExtractTemplateKeysRegex()).To(Equal([]string{"clusterDomain", "namespace", "env"}))
		})

		It("returns an empty slice when DNSConfig is nil or contains no matches", func() {
			dnsClass := &DNSClass{}
			Expect(dnsClass.ExtractTemplateKeysRegex()).To(BeEmpty())

			dnsClass.Spec.DNSConfig = &corev1.PodDNSConfig{
				Searches: []string{
					"example.com",
					"{{ clusterDomain }}",
				},
			}
			Expect(dnsClass.ExtractTemplateKeysRegex()).To(BeEmpty())
		})
	})
})
