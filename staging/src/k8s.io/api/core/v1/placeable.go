package v1

import "k8s.io/apimachinery/pkg/apis/meta/v1"
import "k8s.io/apimachinery/pkg/runtime/schema"
import "k8s.io/apimachinery/pkg/types"
import "k8s.io/apimachinery/pkg/runtime"

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

	GetInitContainers() []Container

	GetContainers() []Container

	GetNodeSelector() map[string]string

	GetNodeName() string

	SetNodeName(x string)

	GetAffinity() *Affinity

	GetSchedulerName() string

	GetTolerations() []Toleration

	GetPriority() *int32


// Generated methods for Status access

	GetPhase() PodPhase
}

func (pod *Pod) GetVolumes() []Volume {return pod.Spec.Volumes}

func (pod *Pod) GetInitContainers() []Container {return pod.Spec.InitContainers}

func (pod *Pod) GetContainers() []Container {return pod.Spec.Containers}

func (pod *Pod) GetNodeSelector() map[string]string {return pod.Spec.NodeSelector}

func (pod *Pod) GetNodeName() string {return pod.Spec.NodeName}

func (pod *Pod) SetNodeName(x string) {pod.Spec.NodeName = x}

func (pod *Pod) GetAffinity() *Affinity {return pod.Spec.Affinity}

func (pod *Pod) GetSchedulerName() string {return pod.Spec.SchedulerName}

func (pod *Pod) GetTolerations() []Toleration {return pod.Spec.Tolerations}

func (pod *Pod) GetPriority() *int32 {return pod.Spec.Priority}

func (pod *Pod) GetPhase() PodPhase {return pod.Status.Phase}
