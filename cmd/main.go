/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"time"

	_ "embed"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	goversion "github.com/caarlos0/go-version"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	configv1alpha1 "github.com/eminaktas/kubedns-shepherd/api/v1alpha1"
	"github.com/eminaktas/kubedns-shepherd/internal/controller"
	webhook_controller "github.com/eminaktas/kubedns-shepherd/internal/webhook"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	version   = ""
	commit    = ""
	treeState = ""
	date      = ""
	builtBy   = ""
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(configv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// LeaderElectionConfiguration defines the configuration of leader election
// clients for components that can run with leader election enabled.
type leaderElectionConfiguration struct {
	// leaderElect enables a leader election client to gain leadership
	// before executing the main loop. Enable this when running replicated
	// components for high availability.
	LeaderElect bool
	// leaseDuration is the duration that non-leader candidates will wait
	// after observing a leadership renewal until attempting to acquire
	// leadership of a led but unrenewed leader slot. This is effectively the
	// maximum duration that a leader can be stopped before it is replaced
	// by another candidate. This is only applicable if leader election is
	// enabled.
	LeaseDuration time.Duration
	// renewDeadline is the interval between attempts by the acting master to
	// renew a leadership slot before it stops leading. This must be less
	// than or equal to the lease duration. This is only applicable if leader
	// election is enabled.
	RenewDeadline time.Duration
	// retryPeriod is the duration the clients should wait between attempting
	// acquisition and renewal of a leadership. This is only applicable if
	// leader election is enabled.
	RetryPeriod time.Duration
	// resourceLock indicates the resource object type that will be used to lock
	// during leader election cycles.
	ResourceLock string
	// resourceName indicates the name of resource object that will be used to lock
	// during leader election cycles.
	ResourceName string
	// resourceNamespace indicates the namespace of resource object that will be used to lock
	// during leader election cycles.
	ResourceNamespace string
}

func main() {
	var printVersion bool
	var metricsAddr string
	var leaderElection leaderElectionConfiguration
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var maxConcurrentReconcilesForDNSClassReconciler int
	flag.BoolVar(&printVersion, "version", false, "Prints the version")
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.IntVar(&maxConcurrentReconcilesForDNSClassReconciler, "max-concurrent-reconciles-for-dnsclass-reconciler", 2,
		"Specifies the maximum number of concurrent reconciles for the DNSClass reconciler.")
	flag.BoolVar(&leaderElection.LeaderElect, "leader-elect", false, ""+
		"Start a leader election client and gain leadership before "+
		"executing the main loop. Enable this when running replicated "+
		"components for high availability.")
	flag.StringVar(&leaderElection.ResourceLock, "leader-elect-resource-lock", "leases", ""+
		"The type of resource object that is used for locking during "+
		"leader election. Supported options are 'leases', 'endpointsleases' "+
		"and 'configmapsleases'.")
	flag.StringVar(&leaderElection.ResourceNamespace, "leader-elect-resource-namespace", "", ""+
		"The namespace of resource object that is used for locking during "+
		"leader election.")
	flag.StringVar(&leaderElection.ResourceName, "leader-elect-resource-name", "kubedns-shepherd.io", ""+
		"The name of resource object that is used for locking during "+
		"leader election.")
	flag.DurationVar(&leaderElection.LeaseDuration, "leader-elect-lease-duration", 15*time.Second, ""+
		"The duration that non-leader candidates will wait after observing a leadership "+
		"renewal until attempting to acquire leadership of a led but unrenewed leader "+
		"slot. This is effectively the maximum duration that a leader can be stopped "+
		"before it is replaced by another candidate. This is only applicable if leader "+
		"election is enabled.")
	flag.DurationVar(&leaderElection.RenewDeadline, "leader-elect-renew-deadline", 10*time.Second, ""+
		"The interval between attempts by the acting master to renew a leadership slot "+
		"before it stops leading. This must be less than the lease duration. "+
		"This is only applicable if leader election is enabled.")
	flag.DurationVar(&leaderElection.RetryPeriod, "leader-elect-retry-period", 2*time.Second, ""+
		"The duration the clients should wait between attempting acquisition and renewal "+
		"of a leadership. This is only applicable if leader election is enabled.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if printVersion {
		buildVersion := buildVersion()
		fmt.Println(buildVersion.String())
		os.Exit(0)
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancelation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:              webhookServer,
		HealthProbeBindAddress:     probeAddr,
		LeaderElection:             leaderElection.LeaderElect,
		LeaderElectionResourceLock: leaderElection.ResourceLock,
		LeaderElectionNamespace:    leaderElection.ResourceNamespace,
		LeaderElectionID:           leaderElection.ResourceName,
		LeaseDuration:              &leaderElection.LeaseDuration,
		RenewDeadline:              &leaderElection.RenewDeadline,
		RetryPeriod:                &leaderElection.RetryPeriod,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	if err = (&controller.DNSClassReconciler{
		Client:        mgr.GetClient(),
		Config:        mgr.GetConfig(),
		EventRecorder: mgr.GetEventRecorderFor("dnsclass-controller"),
		MaxConcurrentReconcilesForDNSClassReconciler: maxConcurrentReconcilesForDNSClassReconciler,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DNSClass")
		os.Exit(1)
	}

	if err = (&configv1alpha1.DNSClass{}).SetupWebhookWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create webhook", "webhook", "DNSClass")
		os.Exit(1)
	}

	mgr.GetWebhookServer().Register("/mutate-v1-pod", &webhook.Admission{
		Handler: &webhook_controller.PodMutator{
			Client:  mgr.GetClient(),
			Decoder: admission.NewDecoder(mgr.GetScheme()),
		},
	})
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

const website = "https://github.com/eminaktas/kubedns-shepherd"

//go:embed "art.txt"
var asciiArt string

func buildVersion() goversion.Info {
	return goversion.GetVersionInfo(
		goversion.WithAppDetails(
			"kubedns-shepherd",
			"A Kubernetes controller that manages the DNS configuration for workloads",
			website),
		goversion.WithASCIIName(asciiArt),
		func(i *goversion.Info) {
			if commit != "" {
				i.GitCommit = commit
			}
			if treeState != "" {
				i.GitTreeState = treeState
			}
			if date != "" {
				i.BuildDate = date
			}
			if version != "" {
				i.GitVersion = version
			}
			if builtBy != "" {
				i.BuiltBy = builtBy
			}
		},
	)
}
