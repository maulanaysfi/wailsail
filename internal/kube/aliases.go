package kube

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

type (
	dynamicResource = dynamic.ResourceInterface
	runtimeObject   = runtime.Object
)
