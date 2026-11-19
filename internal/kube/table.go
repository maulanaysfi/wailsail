package kube

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ResourceRef identifies a Kubernetes resource type.
type ResourceRef struct {
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
}

// GroupVersion returns "v1" or "group/version".
func (r ResourceRef) GroupVersion() string {
	if r.Group == "" {
		return r.Version
	}
	return r.Group + "/" + r.Version
}

// Path builds the API path for a collection (name == "") or a single object.
func (r ResourceRef) Path(namespace, name string) string {
	var b strings.Builder
	if r.Group == "" {
		b.WriteString("/api/")
		b.WriteString(r.Version)
	} else {
		b.WriteString("/apis/")
		b.WriteString(r.Group)
		b.WriteString("/")
		b.WriteString(r.Version)
	}
	if r.Namespaced && namespace != "" {
		b.WriteString("/namespaces/")
		b.WriteString(url.PathEscape(namespace))
	}
	b.WriteString("/")
	b.WriteString(r.Resource)
	if name != "" {
		b.WriteString("/")
		b.WriteString(url.PathEscape(name))
	}
	return b.String()
}

// Column is a table column as rendered by the API server.
type Column struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Priority    int32  `json:"priority"`
}

// OwnerRef identifies the controlling owner of an object, if any.
type OwnerRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// PodResources holds requests and limits for a pod's main container.
type PodResources struct {
	Container     string `json:"container,omitempty"`
	CPURequest    string `json:"cpuRequest,omitempty"`
	CPULimit      string `json:"cpuLimit,omitempty"`
	MemoryRequest string `json:"memoryRequest,omitempty"`
	MemoryLimit   string `json:"memoryLimit,omitempty"`
}

// Row is a compact, display-ready view of one object.
type Row struct {
	Key          string        `json:"key"`
	UID          string        `json:"uid"`
	Name         string        `json:"name"`
	Namespace    string        `json:"namespace"`
	Created      string        `json:"created"`
	Cells        []string      `json:"cells"`
	Owner        *OwnerRef     `json:"owner,omitempty"`
	PodResources *PodResources `json:"podResources,omitempty"`
}

// key returns a stable identity for a row (UID, or namespace/name fallback).
func (r Row) key() string {
	if r.UID != "" {
		return r.UID
	}
	return r.Namespace + "/" + r.Name
}

// tableResult is the parsed form of a Table (or plain list) response.
type tableResult struct {
	Columns         []Column
	Rows            []Row
	ResourceVersion string
	Continue        string
	// colIndex maps kept columns to their index in the server cells.
	colIndex []int
}

// AcceptTable asks the API server for server-side printed tables, falling
// back to plain JSON for resources that do not support it.
const AcceptTable = "application/json;as=Table;v=v1;g=meta.k8s.io,application/json"

// skippedColumn reports columns wailsail renders itself.
func skippedColumn(name string) bool {
	return name == "Age"
}

func cellString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(b)
	}
}

type objectMeta struct {
	Kind     string            `json:"kind"`
	Metadata metav1.ObjectMeta `json:"metadata"`
	Spec     struct {
		Containers []struct {
			Name      string `json:"name"`
			Resources struct {
				Requests map[string]string `json:"requests"`
				Limits   map[string]string `json:"limits"`
			} `json:"resources"`
		} `json:"containers"`
	} `json:"spec"`
}

// rowFromMeta builds identity fields from an object's metadata.
func rowFromMeta(m metav1.ObjectMeta) Row {
	r := Row{UID: string(m.UID), Name: m.Name, Namespace: m.Namespace}
	if !m.CreationTimestamp.IsZero() {
		r.Created = m.CreationTimestamp.UTC().Format("2006-01-02T15:04:05Z")
	}
	if len(m.OwnerReferences) > 0 {
		pick := m.OwnerReferences[0]
		for _, o := range m.OwnerReferences {
			if o.Controller != nil && *o.Controller {
				pick = o
				break
			}
		}
		r.Owner = &OwnerRef{APIVersion: pick.APIVersion, Kind: pick.Kind, Name: pick.Name}
	}
	r.Key = r.key()
	return r
}

// parseTable decodes a Table response. If the server returned a plain list
// or a single object instead, it degrades to name-only rows.
// prevColumns allows watch events (which may omit column definitions) to
// reuse the column layout from the initial list.
func parseTable(raw []byte, prev *tableResult) (*tableResult, error) {
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if probe.Kind != "Table" {
		return parsePlain(raw, probe.Kind)
	}
	var t metav1.Table
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, err
	}
	res := &tableResult{ResourceVersion: t.ResourceVersion, Continue: t.Continue}
	if len(t.ColumnDefinitions) > 0 {
		for i, c := range t.ColumnDefinitions {
			if skippedColumn(c.Name) {
				continue
			}
			res.Columns = append(res.Columns, Column{
				Name: c.Name, Type: c.Type, Description: c.Description, Priority: c.Priority,
			})
			res.colIndex = append(res.colIndex, i)
		}
	} else if prev != nil {
		res.Columns = prev.Columns
		res.colIndex = prev.colIndex
	}
	for _, tr := range t.Rows {
		var om objectMeta
		if len(tr.Object.Raw) > 0 {
			_ = json.Unmarshal(tr.Object.Raw, &om)
		}
		row := rowFromMeta(om.Metadata)
		if len(om.Spec.Containers) > 0 {
			c0 := om.Spec.Containers[0]
			row.PodResources = &PodResources{
				Container:     c0.Name,
				CPURequest:    c0.Resources.Requests["cpu"],
				CPULimit:      c0.Resources.Limits["cpu"],
				MemoryRequest: c0.Resources.Requests["memory"],
				MemoryLimit:   c0.Resources.Limits["memory"],
			}
		}
		if res.ResourceVersion == "" && om.Metadata.ResourceVersion != "" {
			res.ResourceVersion = om.Metadata.ResourceVersion
		}
		row.Cells = make([]string, len(res.colIndex))
		for i, ci := range res.colIndex {
			if ci < len(tr.Cells) {
				row.Cells[i] = cellString(tr.Cells[ci])
			}
		}
		if row.Name == "" && len(tr.Cells) > 0 {
			row.Name = cellString(tr.Cells[0])
			row.Key = row.key()
		}
		res.Rows = append(res.Rows, row)
	}
	return res, nil
}

func parsePlain(raw []byte, kind string) (*tableResult, error) {
	res := &tableResult{Columns: []Column{{Name: "Name", Type: "string"}}, colIndex: []int{0}}
	if strings.HasSuffix(kind, "List") {
		var list struct {
			Metadata metav1.ListMeta  `json:"metadata"`
			Items    []objectMeta     `json:"items"`
		}
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		res.ResourceVersion = list.Metadata.ResourceVersion
		res.Continue = list.Metadata.Continue
		for _, it := range list.Items {
			row := rowFromMeta(it.Metadata)
			row.Cells = []string{row.Name}
			res.Rows = append(res.Rows, row)
		}
		return res, nil
	}
	var one objectMeta
	if err := json.Unmarshal(raw, &one); err != nil {
		return nil, err
	}
	row := rowFromMeta(one.Metadata)
	row.Cells = []string{row.Name}
	res.Rows = []Row{row}
	res.ResourceVersion = one.Metadata.ResourceVersion
	return res, nil
}
