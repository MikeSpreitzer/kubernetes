package v1

import "k8s.io/apimachinery/pkg/runtime"
import "k8s.io/apimachinery/pkg/apis/meta/v1"
import "k8s.io/apimachinery/pkg/runtime/schema"
import "k8s.io/apimachinery/pkg/types"

type Placeable interface {

	DeepCopy() *Pod

	DeepCopyInto(out *Pod)

	DeepCopyObject() runtime.Object

	Descriptor() ([]byte, []int)

	GetAnnotations() map[string]string

	GetClusterName() string

	GetCreationTimestamp() v1.Time

	GetDeletionGracePeriodSeconds() *int64

	GetDeletionTimestamp() *v1.Time

	GetFinalizers() []string

	GetGenerateName() string

	GetGeneration() int64

	GetInitializers() *v1.Initializers

	GetLabels() map[string]string

	GetName() string

	GetNamespace() string

	GetObjectKind() schema.ObjectKind

	GetObjectMeta() v1.Object

	GetOwnerReferences() []v1.OwnerReference

	GetResourceVersion() string

	GetSelfLink() string

	GetUID() types.UID

	GroupVersionKind() schema.GroupVersionKind

	Marshal() (dAtA []byte, err error)

	MarshalTo(dAtA []byte) (int, error)

	ProtoMessage()

	Reset()

	SetAnnotations(annotations map[string]string)

	SetClusterName(clusterName string)

	SetCreationTimestamp(creationTimestamp v1.Time)

	SetDeletionGracePeriodSeconds(deletionGracePeriodSeconds *int64)

	SetDeletionTimestamp(deletionTimestamp *v1.Time)

	SetFinalizers(finalizers []string)

	SetGenerateName(generateName string)

	SetGeneration(generation int64)

	SetGroupVersionKind(gvk schema.GroupVersionKind)

	SetInitializers(initializers *v1.Initializers)

	SetLabels(labels map[string]string)

	SetName(name string)

	SetNamespace(namespace string)

	SetOwnerReferences(references []v1.OwnerReference)

	SetResourceVersion(version string)

	SetSelfLink(selfLink string)

	SetUID(uid types.UID)

	Size() (n int)

	String() string

	SwaggerDoc() map[string]string

	Unmarshal(dAtA []byte) error


// Generated methods for Spec access

	GetVolumes() []Volume

	SetVolumes(x []Volume)

	GetInitContainers() []Container

	SetInitContainers(x []Container)

	GetContainers() []Container

	SetContainers(x []Container)

	GetRestartPolicy() RestartPolicy

	SetRestartPolicy(x RestartPolicy)

	GetTerminationGracePeriodSeconds() *int64

	SetTerminationGracePeriodSeconds(x *int64)

	GetActiveDeadlineSeconds() *int64

	SetActiveDeadlineSeconds(x *int64)

	GetDNSPolicy() DNSPolicy

	SetDNSPolicy(x DNSPolicy)

	GetNodeSelector() map[string]string

	SetNodeSelector(x map[string]string)

	GetServiceAccountName() string

	SetServiceAccountName(x string)

	GetDeprecatedServiceAccount() string

	SetDeprecatedServiceAccount(x string)

	GetAutomountServiceAccountToken() *bool

	SetAutomountServiceAccountToken(x *bool)

	GetNodeName() string

	SetNodeName(x string)

	GetHostNetwork() bool

	SetHostNetwork(x bool)

	GetHostPID() bool

	SetHostPID(x bool)

	GetHostIPC() bool

	SetHostIPC(x bool)

	GetSecurityContext() *PodSecurityContext

	SetSecurityContext(x *PodSecurityContext)

	GetImagePullSecrets() []LocalObjectReference

	SetImagePullSecrets(x []LocalObjectReference)

	GetHostname() string

	SetHostname(x string)

	GetSubdomain() string

	SetSubdomain(x string)

	GetAffinity() *Affinity

	SetAffinity(x *Affinity)

	GetSchedulerName() string

	SetSchedulerName(x string)

	GetTolerations() []Toleration

	SetTolerations(x []Toleration)

	GetHostAliases() []HostAlias

	SetHostAliases(x []HostAlias)

	GetPriorityClassName() string

	SetPriorityClassName(x string)

	GetPriority() *int32

	SetPriority(x *int32)


// Generated methods for Status access

	GetPhase() PodPhase

	SetPhase(x PodPhase)

	GetConditions() []PodCondition

	SetConditions(x []PodCondition)

	GetMessage() string

	SetMessage(x string)

	GetReason() string

	SetReason(x string)

	GetHostIP() string

	SetHostIP(x string)

	GetPodIP() string

	SetPodIP(x string)

	GetStartTime() *v1.Time

	SetStartTime(x *v1.Time)

	GetInitContainerStatuses() []ContainerStatus

	SetInitContainerStatuses(x []ContainerStatus)

	GetContainerStatuses() []ContainerStatus

	SetContainerStatuses(x []ContainerStatus)

	GetQOSClass() PodQOSClass

	SetQOSClass(x PodQOSClass)
}

func (pod *Pod) GetVolumes() []Volume {return pod.Spec.Volumes}

func (pod *Pod) SetVolumes(x []Volume) {pod.Spec.Volumes = x}

func (pod *Pod) GetInitContainers() []Container {return pod.Spec.InitContainers}

func (pod *Pod) SetInitContainers(x []Container) {pod.Spec.InitContainers = x}

func (pod *Pod) GetContainers() []Container {return pod.Spec.Containers}

func (pod *Pod) SetContainers(x []Container) {pod.Spec.Containers = x}

func (pod *Pod) GetRestartPolicy() RestartPolicy {return pod.Spec.RestartPolicy}

func (pod *Pod) SetRestartPolicy(x RestartPolicy) {pod.Spec.RestartPolicy = x}

func (pod *Pod) GetTerminationGracePeriodSeconds() *int64 {return pod.Spec.TerminationGracePeriodSeconds}

func (pod *Pod) SetTerminationGracePeriodSeconds(x *int64) {pod.Spec.TerminationGracePeriodSeconds = x}

func (pod *Pod) GetActiveDeadlineSeconds() *int64 {return pod.Spec.ActiveDeadlineSeconds}

func (pod *Pod) SetActiveDeadlineSeconds(x *int64) {pod.Spec.ActiveDeadlineSeconds = x}

func (pod *Pod) GetDNSPolicy() DNSPolicy {return pod.Spec.DNSPolicy}

func (pod *Pod) SetDNSPolicy(x DNSPolicy) {pod.Spec.DNSPolicy = x}

func (pod *Pod) GetNodeSelector() map[string]string {return pod.Spec.NodeSelector}

func (pod *Pod) SetNodeSelector(x map[string]string) {pod.Spec.NodeSelector = x}

func (pod *Pod) GetServiceAccountName() string {return pod.Spec.ServiceAccountName}

func (pod *Pod) SetServiceAccountName(x string) {pod.Spec.ServiceAccountName = x}

func (pod *Pod) GetDeprecatedServiceAccount() string {return pod.Spec.DeprecatedServiceAccount}

func (pod *Pod) SetDeprecatedServiceAccount(x string) {pod.Spec.DeprecatedServiceAccount = x}

func (pod *Pod) GetAutomountServiceAccountToken() *bool {return pod.Spec.AutomountServiceAccountToken}

func (pod *Pod) SetAutomountServiceAccountToken(x *bool) {pod.Spec.AutomountServiceAccountToken = x}

func (pod *Pod) GetNodeName() string {return pod.Spec.NodeName}

func (pod *Pod) SetNodeName(x string) {pod.Spec.NodeName = x}

func (pod *Pod) GetHostNetwork() bool {return pod.Spec.HostNetwork}

func (pod *Pod) SetHostNetwork(x bool) {pod.Spec.HostNetwork = x}

func (pod *Pod) GetHostPID() bool {return pod.Spec.HostPID}

func (pod *Pod) SetHostPID(x bool) {pod.Spec.HostPID = x}

func (pod *Pod) GetHostIPC() bool {return pod.Spec.HostIPC}

func (pod *Pod) SetHostIPC(x bool) {pod.Spec.HostIPC = x}

func (pod *Pod) GetSecurityContext() *PodSecurityContext {return pod.Spec.SecurityContext}

func (pod *Pod) SetSecurityContext(x *PodSecurityContext) {pod.Spec.SecurityContext = x}

func (pod *Pod) GetImagePullSecrets() []LocalObjectReference {return pod.Spec.ImagePullSecrets}

func (pod *Pod) SetImagePullSecrets(x []LocalObjectReference) {pod.Spec.ImagePullSecrets = x}

func (pod *Pod) GetHostname() string {return pod.Spec.Hostname}

func (pod *Pod) SetHostname(x string) {pod.Spec.Hostname = x}

func (pod *Pod) GetSubdomain() string {return pod.Spec.Subdomain}

func (pod *Pod) SetSubdomain(x string) {pod.Spec.Subdomain = x}

func (pod *Pod) GetAffinity() *Affinity {return pod.Spec.Affinity}

func (pod *Pod) SetAffinity(x *Affinity) {pod.Spec.Affinity = x}

func (pod *Pod) GetSchedulerName() string {return pod.Spec.SchedulerName}

func (pod *Pod) SetSchedulerName(x string) {pod.Spec.SchedulerName = x}

func (pod *Pod) GetTolerations() []Toleration {return pod.Spec.Tolerations}

func (pod *Pod) SetTolerations(x []Toleration) {pod.Spec.Tolerations = x}

func (pod *Pod) GetHostAliases() []HostAlias {return pod.Spec.HostAliases}

func (pod *Pod) SetHostAliases(x []HostAlias) {pod.Spec.HostAliases = x}

func (pod *Pod) GetPriorityClassName() string {return pod.Spec.PriorityClassName}

func (pod *Pod) SetPriorityClassName(x string) {pod.Spec.PriorityClassName = x}

func (pod *Pod) GetPriority() *int32 {return pod.Spec.Priority}

func (pod *Pod) SetPriority(x *int32) {pod.Spec.Priority = x}

func (pod *Pod) GetPhase() PodPhase {return pod.Status.Phase}

func (pod *Pod) SetPhase(x PodPhase) {pod.Status.Phase = x}

func (pod *Pod) GetConditions() []PodCondition {return pod.Status.Conditions}

func (pod *Pod) SetConditions(x []PodCondition) {pod.Status.Conditions = x}

func (pod *Pod) GetMessage() string {return pod.Status.Message}

func (pod *Pod) SetMessage(x string) {pod.Status.Message = x}

func (pod *Pod) GetReason() string {return pod.Status.Reason}

func (pod *Pod) SetReason(x string) {pod.Status.Reason = x}

func (pod *Pod) GetHostIP() string {return pod.Status.HostIP}

func (pod *Pod) SetHostIP(x string) {pod.Status.HostIP = x}

func (pod *Pod) GetPodIP() string {return pod.Status.PodIP}

func (pod *Pod) SetPodIP(x string) {pod.Status.PodIP = x}

func (pod *Pod) GetStartTime() *v1.Time {return pod.Status.StartTime}

func (pod *Pod) SetStartTime(x *v1.Time) {pod.Status.StartTime = x}

func (pod *Pod) GetInitContainerStatuses() []ContainerStatus {return pod.Status.InitContainerStatuses}

func (pod *Pod) SetInitContainerStatuses(x []ContainerStatus) {pod.Status.InitContainerStatuses = x}

func (pod *Pod) GetContainerStatuses() []ContainerStatus {return pod.Status.ContainerStatuses}

func (pod *Pod) SetContainerStatuses(x []ContainerStatus) {pod.Status.ContainerStatuses = x}

func (pod *Pod) GetQOSClass() PodQOSClass {return pod.Status.QOSClass}

func (pod *Pod) SetQOSClass(x PodQOSClass) {pod.Status.QOSClass = x}
