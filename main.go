/*
Copyright 2022.

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
	"flag"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	csv1alpha1 "github.com/opensourceways/code-server-operator/api/v1alpha1"
	"github.com/opensourceways/code-server-operator/controllers"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	REQUEST_CHAN_SIZE = 10
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(csv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	csOption := controllers.CodeServerOption{}

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&csOption.DomainName, "domain-name", "pool1.playground.osinfra.cn", "Code server domain name.")
	flag.StringVar(&csOption.VSExporterImage, "vs-default-exporter", "ghcr.io/artificial-aidan/active-exporter:latest",
		"Default exporter image used as a code server sidecar for VS code instance.")
	flag.IntVar(&csOption.ProbeInterval, "probe-interval", 20,
		"time in seconds between two probes on code server instance.")
	flag.IntVar(&csOption.MaxProbeRetry, "max-probe-retry", 10,
		"count before marking code server inactive when failed to probe liveness")
	flag.StringVar(&csOption.HttpsSecretName, "secret-name", "code-server-secret", "Secret which holds the https cert(tls.crt) and key file(tls.key). This secret will be used in ingress controller as well as code server instance.")
	flag.StringVar(&csOption.LxdClientSecretName, "lxd-client-secret-name", "lxd-client-secret", "Secret which holds the key and secret for lxc client to communicate to server.")
	flag.BoolVar(&csOption.EnableUserIngress, "enable-user-ingress", false, "enable user ingress for visiting.")
	flag.IntVar(&csOption.MaxConcurrency, "max-concurrency", 10, "Max concurrency of reconcile worker.")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "beb50962.opensourceways.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	csRequest := make(chan controllers.CodeServerRequest, REQUEST_CHAN_SIZE)

	if err = (&controllers.CodeServerReconciler{
		Client:  mgr.GetClient(),
		Scheme:  mgr.GetScheme(),
		Log:     ctrl.Log.WithName("controllers").WithName("CodeServer"),
		Options: &csOption,
		ReqCh:   csRequest,
	}).SetupWithManager(mgr, csOption.MaxConcurrency); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CodeServer")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder
	probeTicker := time.NewTicker(time.Duration(csOption.ProbeInterval) * time.Second)
	defer probeTicker.Stop()
	//setup code server watcher
	codeServerWatcher := controllers.NewCodeServerWatcher(
		mgr.GetClient(),
		ctrl.Log.WithName("controllers").WithName("CodeServerWatcher"),
		mgr.GetScheme(),
		&csOption,
		csRequest,
		probeTicker.C)
	stopContext := ctrl.SetupSignalHandler()
	go codeServerWatcher.Run(stopContext.Done())

	setupLog.Info("starting manager")
	if err := mgr.Start(stopContext); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
