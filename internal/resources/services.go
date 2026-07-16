package resources

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Typed Service detail (Sprint 7, Story 7.2). The generic engine serves the
// Service object and its YAML; this hot-path endpoint adds the one thing the raw
// object cannot carry — its resolved Endpoints, split into ready and not-ready
// backing addresses, each linked to the pod behind it (targetRef). Together with
// the selector this is the Service's "matching pod list": the ready/not-ready
// addresses ARE the pods the selector currently resolves to.

// ServicePortSummary is one shaped Service port.
type ServicePortSummary struct {
	Name       string `json:"name,omitempty"`
	Port       int32  `json:"port"`
	Protocol   string `json:"protocol"`
	TargetPort string `json:"targetPort,omitempty"`
	NodePort   int32  `json:"nodePort,omitempty"`
}

// EndpointTargetRef points a resolved endpoint address at the pod behind it, so
// the UI can deep-link to that pod's detail view.
type EndpointTargetRef struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// EndpointAddressSummary is one backing address (a pod IP) with its target and
// ready state.
type EndpointAddressSummary struct {
	IP        string             `json:"ip"`
	Hostname  string             `json:"hostname,omitempty"`
	NodeName  string             `json:"nodeName,omitempty"`
	Ready     bool               `json:"ready"`
	TargetRef *EndpointTargetRef `json:"targetRef,omitempty"`
}

// ServiceDetail is the typed Service detail payload: the service summary plus its
// resolved endpoints (ready + not-ready addresses).
type ServiceDetail struct {
	Name              string                   `json:"name"`
	Namespace         string                   `json:"namespace"`
	Type              string                   `json:"type"`
	ClusterIP         string                   `json:"clusterIP,omitempty"`
	ClusterIPs        []string                 `json:"clusterIPs,omitempty"`
	ExternalIPs       []string                 `json:"externalIPs,omitempty"`
	SessionAffinity   string                   `json:"sessionAffinity,omitempty"`
	Selector          map[string]string        `json:"selector,omitempty"`
	Ports             []ServicePortSummary     `json:"ports"`
	EndpointsFound    bool                     `json:"endpointsFound"`
	ReadyAddresses    []EndpointAddressSummary `json:"readyAddresses"`
	NotReadyAddresses []EndpointAddressSummary `json:"notReadyAddresses"`
}

// ServiceDetailHandler serves GET /api/v1/services/{namespace}/{name}: the
// Service summary plus its resolved Endpoints. A missing Endpoints object (a
// headless service, or one with no backing pods yet) is not an error — it yields
// endpointsFound=false with empty address lists, so the UI shows a meaningful
// "no endpoints" state rather than failing.
func ServiceDetailHandler(cluster Cluster, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		namespace := chi.URLParam(r, "namespace")
		name := chi.URLParam(r, "name")

		clientset, err := cluster.Clientset()
		if err != nil {
			writeKubeconfigUnavailable(w, logger, err)
			return
		}

		ctx := r.Context()
		svc, err := clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			writeEngineError(w, logger, fmt.Sprintf("getting service %q", name), err, classifierFor(cluster))
			return
		}

		detail := shapeService(svc)

		endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			// No Endpoints object: headless or no backing pods. Not an error.
		case err != nil:
			writeEngineError(w, logger, fmt.Sprintf("resolving endpoints for service %q", name), err, classifierFor(cluster))
			return
		default:
			detail.EndpointsFound = true
			detail.ReadyAddresses, detail.NotReadyAddresses = shapeEndpoints(endpoints)
		}

		writeJSON(w, logger, http.StatusOK, detail)
	}
}

// shapeService flattens the Service spec into the detail summary (pure).
func shapeService(svc *corev1.Service) ServiceDetail {
	ports := make([]ServicePortSummary, 0, len(svc.Spec.Ports))
	for _, p := range svc.Spec.Ports {
		proto := string(p.Protocol)
		if proto == "" {
			proto = "TCP"
		}
		ports = append(ports, ServicePortSummary{
			Name:       p.Name,
			Port:       p.Port,
			Protocol:   proto,
			TargetPort: p.TargetPort.String(),
			NodePort:   p.NodePort,
		})
	}
	typ := string(svc.Spec.Type)
	if typ == "" {
		typ = string(corev1.ServiceTypeClusterIP)
	}
	return ServiceDetail{
		Name:              svc.Name,
		Namespace:         svc.Namespace,
		Type:              typ,
		ClusterIP:         svc.Spec.ClusterIP,
		ClusterIPs:        svc.Spec.ClusterIPs,
		ExternalIPs:       svc.Spec.ExternalIPs,
		SessionAffinity:   string(svc.Spec.SessionAffinity),
		Selector:          svc.Spec.Selector,
		Ports:             ports,
		ReadyAddresses:    []EndpointAddressSummary{},
		NotReadyAddresses: []EndpointAddressSummary{},
	}
}

// shapeEndpoints flattens an Endpoints object's subsets into ready and not-ready
// address lists (pure). Addresses are sorted by IP for a stable render.
func shapeEndpoints(ep *corev1.Endpoints) (ready, notReady []EndpointAddressSummary) {
	ready = []EndpointAddressSummary{}
	notReady = []EndpointAddressSummary{}
	for i := range ep.Subsets {
		subset := &ep.Subsets[i]
		for j := range subset.Addresses {
			ready = append(ready, shapeEndpointAddress(&subset.Addresses[j], true))
		}
		for j := range subset.NotReadyAddresses {
			notReady = append(notReady, shapeEndpointAddress(&subset.NotReadyAddresses[j], false))
		}
	}
	sortAddresses(ready)
	sortAddresses(notReady)
	return ready, notReady
}

func shapeEndpointAddress(addr *corev1.EndpointAddress, ready bool) EndpointAddressSummary {
	out := EndpointAddressSummary{IP: addr.IP, Hostname: addr.Hostname, Ready: ready}
	if addr.NodeName != nil {
		out.NodeName = *addr.NodeName
	}
	if addr.TargetRef != nil {
		out.TargetRef = &EndpointTargetRef{
			Kind:      addr.TargetRef.Kind,
			Namespace: addr.TargetRef.Namespace,
			Name:      addr.TargetRef.Name,
		}
	}
	return out
}

func sortAddresses(addrs []EndpointAddressSummary) {
	sort.Slice(addrs, func(i, j int) bool { return addrs[i].IP < addrs[j].IP })
}
