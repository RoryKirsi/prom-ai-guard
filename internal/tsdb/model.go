// Package tsdb is an in-memory model of the Prometheus TSDB label index. It
// aggregates MetricSeries by metric name to expose per-metric series counts,
// label cardinality, fingerprints and the service/job/namespace values needed
// by the rule engine. It does not parse real TSDB block/WAL/chunk binaries.
package tsdb

import (
	"hash/fnv"
	"sort"

	"prom-ai-guard/internal/model"
)

// MetricStat is the aggregated view of one metric name.
type MetricStat struct {
	MetricName           string
	SeriesCount          int
	DuplicateFingerprint bool

	labelValues  map[string]map[string]struct{}
	emptyValue   map[string]bool
	services     map[string]struct{}
	jobs         map[string]struct{}
	namespaces   map[string]struct{}
	fingerprints map[uint64]int
}

func newStat(name string) *MetricStat {
	return &MetricStat{
		MetricName:   name,
		labelValues:  map[string]map[string]struct{}{},
		emptyValue:   map[string]bool{},
		services:     map[string]struct{}{},
		jobs:         map[string]struct{}{},
		namespaces:   map[string]struct{}{},
		fingerprints: map[uint64]int{},
	}
}

// Build aggregates parsed series into per-metric stats keyed by metric name.
func Build(series []model.MetricSeries) map[string]*MetricStat {
	out := make(map[string]*MetricStat)
	for _, s := range series {
		st := out[s.MetricName]
		if st == nil {
			st = newStat(s.MetricName)
			out[s.MetricName] = st
		}
		st.add(s)
	}
	for _, st := range out {
		for _, c := range st.fingerprints {
			if c > 1 {
				st.DuplicateFingerprint = true
				break
			}
		}
	}
	return out
}

func (m *MetricStat) add(s model.MetricSeries) {
	m.SeriesCount++
	for k, v := range s.Labels {
		set := m.labelValues[k]
		if set == nil {
			set = map[string]struct{}{}
			m.labelValues[k] = set
		}
		set[v] = struct{}{}
		if v == "" {
			m.emptyValue[k] = true
		}
		switch k {
		case "service":
			if v != "" {
				m.services[v] = struct{}{}
			}
		case "job":
			if v != "" {
				m.jobs[v] = struct{}{}
			}
		case "namespace":
			if v != "" {
				m.namespaces[v] = struct{}{}
			}
		}
	}
	m.fingerprints[fingerprint(s)]++
}

// fingerprint is a stable per-series identifier: metric name plus label
// key=value pairs sorted by label key, so order does not affect the result.
func fingerprint(s model.MetricSeries) uint64 {
	keys := make([]string, 0, len(s.Labels))
	for k := range s.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := fnv.New64a()
	h.Write([]byte(s.MetricName))
	for _, k := range keys {
		h.Write([]byte{0xff})
		h.Write([]byte(k))
		h.Write([]byte{'='})
		h.Write([]byte(s.Labels[k]))
	}
	return h.Sum64()
}

// LabelKeys returns this metric's label keys, sorted.
func (m *MetricStat) LabelKeys() []string {
	keys := make([]string, 0, len(m.labelValues))
	for k := range m.labelValues {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// LabelCardinality returns the distinct value count per label key.
func (m *MetricStat) LabelCardinality() map[string]int {
	out := make(map[string]int, len(m.labelValues))
	for k, set := range m.labelValues {
		out[k] = len(set)
	}
	return out
}

// DistinctValues returns the number of distinct values seen for a label key.
func (m *MetricStat) DistinctValues(key string) int {
	return len(m.labelValues[key])
}

// HasEmptyValue reports whether the label key ever had an empty value.
func (m *MetricStat) HasEmptyValue(key string) bool {
	return m.emptyValue[key]
}

// EmptyValueKeys returns the label keys that had at least one empty value, sorted.
func (m *MetricStat) EmptyValueKeys() []string {
	return sortedSet(m.emptyValue)
}

// ServiceValues returns distinct service label values, sorted.
func (m *MetricStat) ServiceValues() []string { return sortedKeys(m.services) }

// JobValues returns distinct job label values, sorted.
func (m *MetricStat) JobValues() []string { return sortedKeys(m.jobs) }

// NamespaceValues returns distinct namespace label values, sorted.
func (m *MetricStat) NamespaceValues() []string { return sortedKeys(m.namespaces) }

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k, v := range set {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
