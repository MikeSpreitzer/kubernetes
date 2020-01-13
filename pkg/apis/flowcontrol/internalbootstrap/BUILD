load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["default-internal.go"],
    importpath = "k8s.io/kubernetes/pkg/apis/flowcontrol/internalbootstrap",
    visibility = ["//visibility:public"],
    deps = [
        "//pkg/apis/flowcontrol:go_default_library",
        "//pkg/apis/flowcontrol/install:go_default_library",
        "//staging/src/k8s.io/api/flowcontrol/v1alpha1:go_default_library",
        "//staging/src/k8s.io/apimachinery/pkg/conversion:go_default_library",
        "//staging/src/k8s.io/apiserver/pkg/apis/flowcontrol/bootstrap:go_default_library",
    ],
)

filegroup(
    name = "package-srcs",
    srcs = glob(["**"]),
    tags = ["automanaged"],
    visibility = ["//visibility:private"],
)

filegroup(
    name = "all-srcs",
    srcs = [":package-srcs"],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
