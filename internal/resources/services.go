package resources

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
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

// Service → ready-endpoint resolution (FB-13). The detail handler above shapes a
// Service's endpoints for display; the service port-forward needs the same
// resolution as *targets*: which pods are ready right now, and on which concrete
// container port. Both read the one authoritative source — the Endpoints object
// the endpoints controller maintains — so the forward balances over exactly the
// backends the detail view shows as ready.

// ServiceBackend is one ready endpoint pod of a Service together with the
// numeric container port that the requested service port maps to on that pod.
type ServiceBackend struct {
	Namespace string
	Pod       string
	Port      uint16
}

// ServicePortNotFoundError reports that a Service exists but declares no port
// with the requested number. A 404-class failure, distinct from a missing
// Service (which surfaces as an apierror NotFound).
type ServicePortNotFoundError struct {
	Namespace string
	Service   string
	Port      int32
}

func (e *ServicePortNotFoundError) Error() string {
	return fmt.Sprintf("service %s/%s declares no port %d", e.Namespace, e.Service, e.Port)
}

// UnsupportedPortProtocolError reports a service port that cannot be forwarded.
// Port-forwarding is TCP-only (the SPDY portforward subresource carries TCP
// streams), so a UDP or SCTP port fails up front instead of binding a listener
// that could never carry the protocol.
type UnsupportedPortProtocolError struct {
	Namespace string
	Service   string
	Port      int32
	Protocol  string
}

func (e *UnsupportedPortProtocolError) Error() string {
	return fmt.Sprintf("service %s/%s port %d is %s; only TCP ports can be forwarded",
		e.Namespace, e.Service, e.Port, e.Protocol)
}

// NoReadyEndpointsError reports that a Service resolved to zero ready backend
// pods. Returned immediately so a caller fails fast rather than standing up a
// listener with nowhere to send traffic.
type NoReadyEndpointsError struct {
	Namespace string
	Service   string
	Port      int32
	// NonPodReady counts ready addresses that exist but have no pod behind them
	// — a selector-less Service with manually managed Endpoints pointing at
	// external addresses. There is something ready, just nothing a port-forward
	// can target, and saying "no ready endpoints" there would be false.
	NonPodReady int
}

func (e *NoReadyEndpointsError) Error() string {
	if e.NonPodReady > 0 {
		return fmt.Sprintf(
			"service %s/%s port %d has %d ready endpoint(s), none backed by a pod; a port-forward can only target pods",
			e.Namespace, e.Service, e.Port, e.NonPodReady)
	}
	return fmt.Sprintf("service %s/%s port %d has no ready endpoints", e.Namespace, e.Service, e.Port)
}

// ResolveServiceBackends resolves (namespace, service, servicePort) to the
// Service's ready endpoint pods, each with the concrete numeric container port
// the service port maps to on that pod.
//
// The per-pod port resolution comes free from the Endpoints object: the
// endpoints controller has already resolved any named targetPort against each
// pod's containers, and pods that resolve the same name to different numbers
// land in different subsets carrying different ports. Only Ready addresses are
// used — not-ready and terminating pods are excluded, exactly as kube-proxy
// excludes them from the ClusterIP rotation.
func ResolveServiceBackends(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, service string,
	servicePort int32,
) ([]ServiceBackend, error) {
	svc, err := clientset.CoreV1().Services(namespace).Get(ctx, service, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting service %s/%s: %w", namespace, service, err)
	}

	port := findServicePort(svc, servicePort)
	if port == nil {
		return nil, &ServicePortNotFoundError{Namespace: namespace, Service: service, Port: servicePort}
	}
	if proto := portProtocol(port); proto != corev1.ProtocolTCP {
		return nil, &UnsupportedPortProtocolError{
			Namespace: namespace, Service: service, Port: servicePort, Protocol: string(proto),
		}
	}

	endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(ctx, service, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		// No Endpoints object: a headless service, or none created yet. Not a
		// missing Service — report it as the empty-rotation case.
		return nil, &NoReadyEndpointsError{Namespace: namespace, Service: service, Port: servicePort}
	case err != nil:
		return nil, fmt.Errorf("resolving endpoints for service %s/%s: %w", namespace, service, err)
	}

	backends, nonPodReady := readyBackends(endpoints, port, namespace)
	if len(backends) == 0 {
		return nil, &NoReadyEndpointsError{
			Namespace: namespace, Service: service, Port: servicePort, NonPodReady: nonPodReady,
		}
	}
	return backends, nil
}

// findServicePort locates a Service's port by number, or nil. One number can
// appear twice when the protocols differ — kube-dns declares 53/UDP *before*
// 53/TCP — so a TCP entry always wins: it is the only one a forward can use. A
// non-TCP match is returned only when no TCP port carries that number, so the
// caller reports the protocol accurately instead of "no such port".
func findServicePort(svc *corev1.Service, port int32) *corev1.ServicePort {
	var nonTCP *corev1.ServicePort
	for i := range svc.Spec.Ports {
		p := &svc.Spec.Ports[i]
		if p.Port != port {
			continue
		}
		if portProtocol(p) == corev1.ProtocolTCP {
			return p
		}
		if nonTCP == nil {
			nonTCP = p
		}
	}
	return nonTCP
}

// portProtocol reads a service port's protocol, defaulting to TCP as the API does.
func portProtocol(p *corev1.ServicePort) corev1.Protocol {
	if p.Protocol == "" {
		return corev1.ProtocolTCP
	}
	return p.Protocol
}

// readyBackends collects one backend per ready address that targets a Pod,
// carrying that subset's resolved numeric port for the requested service port.
// Ordered by pod name for a deterministic rotation. Also reports how many ready
// addresses were skipped for having no pod behind them, so an empty result can
// say which of the two situations it is.
func readyBackends(ep *corev1.Endpoints, svcPort *corev1.ServicePort, namespace string) ([]ServiceBackend, int) {
	out := []ServiceBackend{}
	nonPod := 0
	for i := range ep.Subsets {
		subset := &ep.Subsets[i]
		port, ok := subsetPort(subset, svcPort)
		if !ok {
			continue
		}
		for j := range subset.Addresses {
			ref := subset.Addresses[j].TargetRef
			if ref == nil || ref.Kind != "Pod" || ref.Name == "" {
				nonPod++ // an endpoint with no pod behind it cannot be forwarded to
				continue
			}
			ns := ref.Namespace
			if ns == "" {
				ns = namespace
			}
			out = append(out, ServiceBackend{Namespace: ns, Pod: ref.Name, Port: port})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pod == out[j].Pod {
			return out[i].Port < out[j].Port
		}
		return out[i].Pod < out[j].Pod
	})
	return out, nonPod
}

// subsetPort finds a subset's port for the requested service port. Endpoints
// ports are keyed by the *service* port's name (empty for a single unnamed
// port), so the name is the correct join for a multi-port service.
func subsetPort(subset *corev1.EndpointSubset, svcPort *corev1.ServicePort) (uint16, bool) {
	for _, p := range subset.Ports {
		if p.Name != svcPort.Name {
			continue
		}
		if p.Port < 1 || p.Port > 65535 {
			return 0, false
		}
		return uint16(p.Port), true
	}
	return 0, false
}
