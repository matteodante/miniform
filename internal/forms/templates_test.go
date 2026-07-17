package forms

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormTemplates(t *testing.T) {
	t.Run("catalog preserves every built-in form contract", func(t *testing.T) {
		expectedFields := map[string][]string{
			"contact":    {"name", "company", "email", "topic", "message", "consent"},
			"feedback":   {"name", "email", "satisfaction", "feature", "comments"},
			"bug-report": {"reporter", "email", "severity", "area", "steps", "expected"},
			"newsletter": {"email", "first_name", "interest", "consent"},
			"waitlist":   {"name", "company", "email", "team_size", "use_case"},
			"blank":      {"field_one", "field_two"},
		}

		templates := GetFormTemplates()
		require.Len(t, templates, len(expectedFields))
		for _, template := range templates {
			t.Run(template.ID, func(t *testing.T) {
				assert.NotEmpty(t, template.Name)
				assert.NotEmpty(t, template.HTML)
				for _, field := range expectedFields[template.ID] {
					assert.Contains(t, template.HTML, fmt.Sprintf(`name="%s"`, field))
				}
			})
		}
	})

	t.Run("render inserts an escaped action and keeps the catalog immutable", func(t *testing.T) {
		template := GetTemplateByID("contact")
		require.NotNil(t, template)

		rendered := template.RenderHTML(`/forms/contact/submit?token=a&next="quoted"`)

		assert.Contains(t, rendered, `token=a&amp;next=&#34;quoted&#34;`)
		assert.Contains(t, template.HTML, formActionPlaceholder)
		assert.Nil(t, GetTemplateByID("missing"))
	})
}
