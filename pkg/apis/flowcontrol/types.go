/*
Copyright 2019 The Kubernetes Authors.

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

package flowcontrol

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These are valid flow-dingtinguisher methods.
const (
	// FlowDistinguisherMethodByUserType specifies that the flow distinguisher is the username in the request.
	// This type is used to provide some insulation between users.
	FlowDistinguisherMethodByUserType FlowDistinguisherMethodType = "ByUser"

	// FlowDistinguisherMethodByNamespaceType specifies that the flow distinguisher is the namespace of the
	// object that the request acts upon. If the object is not namespaced, or if the request is a non-resource
	// request, then the distinguisher will be the empty string. An example usage of this type is to provide
	// some insulation between tenants in a situation where there are multiple tenants and each namespace
	// is dedicated to a tenant.
	FlowDistinguisherMethodByNamespaceType FlowDistinguisherMethodType = "ByNamespace"

	GroupKind          = "Group"
	ServiceAccountKind = "ServiceAccount"
	UserKind           = "User"

	APIGroupAll    = "*"
	ResourceAll    = "*"
	VerbAll        = "*"
	NonResourceAll = "*"

	NameAll = "*"
)

// System preset priority level names
const (
	PriorityLevelConfigurationNameExempt = "exempt"
)

// Conditions
const (
	FlowSchemaConditionDangling = "Dangling"

	PriorityLevelConfigurationConditionConcurrencyShared = "ConcurrencyShared"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FlowSchema defines the schema of a group of flows. Note that a flow is made up of a set of inbound API requests with
// similar attributes and is identified by a pair of strings: the name of the FlowSchema and a "flow distinguisher".
type FlowSchema struct {
	metav1.TypeMeta
	// `metadata` is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta
	// `spec` is the specification of the desired behavior of a flow-schema.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec FlowSchemaSpec
	// `status` is the current status of a flow-schema.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Status FlowSchemaStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FlowSchemaList is a list of FlowSchema objects.
type FlowSchemaList struct {
	metav1.TypeMeta
	// `metadata` is the standard list metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta

	// `items` is a list of flow-schemas.
	Items []FlowSchema
}

// FlowSchemaSpec describes how the flow-schema's specification looks like.
type FlowSchemaSpec struct {
	// `priorityLevelConfiguration` should reference a PriorityLevelConfiguration in the cluster. If the reference cannot
	// be resolved, the flow-schema will be ignored and marked as invalid in its status.
	// Required.
	PriorityLevelConfiguration PriorityLevelConfigurationReference
	// `matchingPrecedence` is used to choose among the FlowSchemas that match a given request. The chosen
	// FlowSchema is among those with the numerically lowest (which we take to be logically highest)
	// MatchingPrecedence.  Each MatchingPrecedence value must be non-negative.
	// Note that if the precedence is not specified or zero, it will be set to 1000 as default.
	// +optional
	MatchingPrecedence int32
	// `distinguisherMethod` defines how to compute the flow distinguisher for requests that match this schema.
	// `nil` specifies that the distinguisher is disabled and thus will always be the empty string.
	// +optional
	DistinguisherMethod *FlowDistinguisherMethod
	// `rules` describes which requests will match this flow schema. This FlowSchema matches a request if and only if
	// at least one member of rules matches the request.
	// There must be at least one rule for this field, or it will be rejected by the api validation.
	// Required.
	Rules []PolicyRulesWithSubjects
}

// FlowDistinguisherMethodType is the type of flow distinguisher method
type FlowDistinguisherMethodType string

// FlowDistinguisherMethod specifies the method of a flow distinguisher.
type FlowDistinguisherMethod struct {
	// `type` is the type of flow distinguisher method
	// The supported types are "ByUser" and "ByNamespace".
	// Required.
	Type FlowDistinguisherMethodType
}

// PriorityLevelConfigurationReference contains information that points to the "request-priority" being used.
type PriorityLevelConfigurationReference struct {
	// `name` is the name of resource being referenced
	// Required.
	Name string
}

// PolicyRulesWithSubjects prescribes a test that applies to a request to an apiserver. The test considers the subject
// making the request, the verb being requested, and the resource to be acted upon. This PolicyRulesWithSubjects matches
// a request if and only if both (a) at least one member of subjects matches the request and (b) at least one member
// of resourceRules or nonResourceRules matches the request.
type PolicyRulesWithSubjects struct {
	// `subjects` is the list of normal user, serviceaccount, or group that this rule cares about.
	// There must be at least one subject for this field, or it will be rejected by the api validation.
	// Required.
	Subjects []Subject
	// `resourceRules` is a slice of ResourcePolicyRules that identify matching requests according to their verb and the
	// resource they request to act upon.
	// +optional
	ResourceRules []ResourcePolicyRule
	// `nonResourceRules` is a list of NonResourcePolicyRules that identify matching requests according to their verb
	// and the non-resource URL they request to act upon.
	// +optional
	NonResourceRules []NonResourcePolicyRule
}

// Subject matches a set of users.
// Syntactically, Subject is a general API object reference.
// Authorization produces a username and a set of groups, and we imagine special kinds of non-namespaced objects,
// User and Group to represent such a username or group.
// The only kind of true object reference that currently will match any users is ServiceAccount.
type Subject struct {
	// `kind` of object being referenced. Values defined by this API group are "User", "Group", and "ServiceAccount".
	// If the kind value is not recognized, the flow-control layer in api-server should report an error.
	// Required.
	Kind string
	// `name` of the object being referenced.  To match regardless of name, use '*'.
	// Required.
	Name string
	// `namespace` of the referenced object.  If the object kind is non-namespace, such as "User" or "Group", and this value is not empty
	// the Authorizer should report an error.
	// +optional
	Namespace string
}

// ResourcePolicyRule is a predicate that matches some resource requests, testing the request's verb and the resource
// that the request seeks to act upon. A ResourcePolicyRule matches a request if and only if: (a) at least one member
// of verbs matches the request, (b) at least one member of apiGroups matches the request, and (c) at least one member
// of resources matches the request.
type ResourcePolicyRule struct {
	// `verbs` is a list of matching verbs and may not be empty.
	// "*" represents all verbs.
	// Required.
	Verbs []string
	// `apiGroups` is a list of matching API groups and may not be empty.
	// "*" represents all api-groups.
	// Required.
	APIGroups []string
	// `resources` is a list of matching resource (i.e., lowercase and plural) with, if desired, subresource. This list
	// may not be empty.
	// "*" represents all resource urls.
	// Required.
	Resources []string
}

// NonResourcePolicyRule is a predicate that matches non-resource requests according to their verb and the non-resource
// URL they request to act upon. A NonResourcePolicyRule matches a request if and only if both (a) at least one member
// of verbs matches the request and (b) at least one member of nonResourceURLs matches the request.
type NonResourcePolicyRule struct {
	// `verbs` is a list of matching verbs and may not be empty.
	// "*" represents all verbs.
	// Required.
	Verbs []string
	// `nonResourceURLs` is a set of partial urls that a user should have access to.
	// "*" represents all non-resource urls.
	// Required.
	NonResourceURLs []string
}

// FlowSchemaStatus represents the current state of a flow-schema.
type FlowSchemaStatus struct {
	// `conditions` is a list of current states of flow-schema.
	// +optional
	Conditions []FlowSchemaCondition
}

// FlowSchemaCondition describes conditions for a flow-schema.
type FlowSchemaCondition struct {
	// `type` is the type of the condition.
	// Required.
	Type FlowSchemaConditionType
	// `status` is the status of the condition.
	// Can be True, False, Unknown.
	// Required.
	Status ConditionStatus
	// `lastTransitionTime` is the last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time
	// `reason` is a unique, one-word, CamelCase reason for the condition's last transition.
	Reason string
	// `message` is a human-readable message indicating details about last transition.
	Message string
}

// FlowSchemaConditionType is a valid value for FlowSchemaStatusCondition.Type
type FlowSchemaConditionType string

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PriorityLevelConfiguration represents the configuration of a priority level.
type PriorityLevelConfiguration struct {
	metav1.TypeMeta
	// `metadata` is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta
	// `spec` is the specification of the desired behavior of a "request-priority".
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec PriorityLevelConfigurationSpec
	// `status` is the current status of a "request-priority".
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Status PriorityLevelConfigurationStatus
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PriorityLevelConfigurationList is a list of PriorityLevelConfiguration objects.
type PriorityLevelConfigurationList struct {
	metav1.TypeMeta
	// `metadata` is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta
	// `items` is a list of request-priorities.
	Items []PriorityLevelConfiguration
}

// PriorityLevelConfigurationSpec is specification of a priority level
type PriorityLevelConfigurationSpec struct {
	// `assuredConcurrencyShares` must be a positive number for a non-exempt priority level. The concurrency limit of an apiserver
	// is divided among the non-exempt priority levels in proportion to their assured concurrency shares. Basically this produces
	// the assured concurrency value (ACV) for each priority level:
	//
	//             ACV(l) = ceil( SCL * ACS(l) / ( sum[priority levels k] ACS(k) ) )
	//
	// if not specified, it will be rejected by the api validation.
	// Required.
	AssuredConcurrencyShares int32
	// `queues` must be a number of queues that belong to a non-exempt PriorityLevelConfiguration object. The queues exist
	// independently at each apiserver. The value must be positive for a non-exempt priority level and setting it to 1
	// disables shufflesharding and makes the distinguisher method irrelevant.
	// if not specified, it will be defaulted to 64.
	// TODO: sugguest a default or a way of deciding on a value.
	// +optional
	Queues int32
	// `handSize` must be a small positive number for applying shuffle sharding. When a request arrives at an apiserver the
	// request flow identifier’s string pair is hashed and the hash value is used to shuffle the queue indices and deal
	// a hand of the size specified here. If empty, the hand size will the be set to 1.
	// NOTE: To figure out a better value for your cluster, please refer to (#76846)[https://github.com/kubernetes/kubernetes/issues/76846#issuecomment-523700960]
	// if not specified, it will be defaulted to 8.
	// +optional
	HandSize int32
	// `queueLengthLimit` must be a length limit applied to each queue belongs to the priority.  The value must be positive
	// for a non-exempt priority level.
	// if not specified, it will be defaulted to 50.
	// +optional
	QueueLengthLimit int32
	// `exempt` defines whether the priority level is exempted or not.  There should be at most one exempt priority level.
	// Being exempt means that requests of that priority are not subject to concurrency limits (and thus are never queued)
	// and do not detract from the concurrency available for non-exempt requests. The field is default to false and only those system
	// preset priority level can be exempt.
	// if not specified, defaulted to non-exempt i.e. false.
	// +optional
	Exempt bool
}

// PriorityLevelConfigurationConditionType is a valid value for PriorityLevelConfigurationStatusCondition.Type
type PriorityLevelConfigurationConditionType string

// PriorityLevelConfigurationStatus represents the current state of a "request-priority".
type PriorityLevelConfigurationStatus struct {
	// `conditions` is the current state of "request-priority".
	Conditions []PriorityLevelConfigurationCondition
}

// PriorityLevelConfigurationCondition defines the condition of priority level.
type PriorityLevelConfigurationCondition struct {
	// `type` is the type of the condition.
	// Required.
	Type PriorityLevelConfigurationConditionType
	// `status` is the status of the condition.
	// Can be True, False, Unknown.
	// Required.
	Status ConditionStatus
	// `lastTransitionTime` is the last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time
	// `reason` is a unique, one-word, CamelCase reason for the condition's last transition.
	Reason string
	// `message` is a human-readable message indicating details about last transition.
	Message string
}

// ConditionStatus is the status of the condition.
type ConditionStatus string

// These are valid condition statuses. "ConditionTrue" means a resource is in the condition.
// "ConditionFalse" means a resource is not in the condition. "ConditionUnknown" means kubernetes
// can't decide if a resource is in the condition or not. In the future, we could add other
// intermediate conditions, e.g. ConditionDegraded.
const (
	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)
