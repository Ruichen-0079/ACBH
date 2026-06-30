package desktop

import "strings"

type PickerFilterGroup struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
}

type PickerRequest struct {
	Title      string              `json:"title"`
	Filter     string              `json:"filter"`
	InitialDir string              `json:"initialDir"`
	Filters    []PickerFilterGroup `json:"filters"`
	Path       string              `json:"path"`
	Paths      []string            `json:"paths"`
}

func pickerFilterFromRequest(req PickerRequest) string {
	if strings.TrimSpace(req.Filter) != "" {
		return req.Filter
	}
	if len(req.Filters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(req.Filters)*2)
	for _, group := range req.Filters {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			name = "Files"
		}
		patterns := make([]string, 0, len(group.Patterns))
		for _, pattern := range group.Patterns {
			pattern = strings.TrimSpace(pattern)
			if pattern != "" {
				patterns = append(patterns, pattern)
			}
		}
		if len(patterns) == 0 {
			continue
		}
		parts = append(parts, name, strings.Join(patterns, ";"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "|")
}