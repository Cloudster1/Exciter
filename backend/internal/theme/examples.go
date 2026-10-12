package theme

// Example is a theme anybody may start from, offered beside a blank sheet: Armature's
// two, value for value, but the colours raised to 4.5:1 as in Cloudster1/Armature#17.
type Example struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Spec        Spec   `json:"spec"`
}

// Examples are the themes shipped with the product.
func Examples() []Example {
	examples := []Example{deepTech(), constellation()}
	for i := range examples {
		examples[i].Spec.normalise()
	}
	return examples
}

// ExampleByKey finds one example, or nil.
func ExampleByKey(key string) *Example {
	for _, e := range Examples() {
		if e.Key == key {
			example := e
			return &example
		}
	}
	return nil
}

func intPtr(n int) *int { return &n }

// constellation is Deep-Tech with a live network of points and lines behind
// the content, and a deeper teal in the dark.
func constellation() Example {
	base := deepTech()
	light := map[string]string{}
	for k, v := range base.Spec.Colors.Light {
		light[k] = v
	}
	dark := map[string]string{}
	for k, v := range base.Spec.Colors.Dark {
		dark[k] = v
	}
	for k, v := range map[string]string{
		"backdrop-from": "#eef3f8", "backdrop-to": "#cfdbe8", "accent": "#00699e", "accent-hover": "#015580",
		"accent-subtle": "#dcedf8", "focus": "#00699e", "selection": "#d3e6f4", "chart-1": "#00699e",
	} {
		light[k] = v
	}
	for k, v := range map[string]string{
		"canvas": "#0a141d", "surface": "#10202c", "surface-raised": "#172b3a", "surface-sunken": "#071017",
		"surface-overlay": "#14283a", "surface-glass": "rgb(16 32 44 / 0.6)", "backdrop-from": "#123141",
		"backdrop-to": "#07111a", "border": "#1f3a4c", "border-strong": "#2f5468",
		"accent": "#4fd1c5", "accent-hover": "#7de3d9", "accent-subtle": "#123a3d", "primary": "#4fd1c5",
		"primary-hover": "#7de3d9", "focus": "#4fd1c5", "selection": "#174549", "chart-1": "#4fd1c5", "chart-5": "#5cc8ff",
	} {
		dark[k] = v
	}
	// The network itself is drawn live by the shell, so nothing in it repeats;
	// the stylesheet keeps the glow it drifts over and the picture's column.
	css := `.bg-backdrop {
  background-color: var(--color-backdrop-to);
  background-image:
    radial-gradient(circle at 15% 10%, var(--color-backdrop-from) 0, transparent 48%),
    radial-gradient(circle at 85% 90%, var(--color-accent-subtle) 0, transparent 42%);
  background-attachment: fixed;
}

/* The picture's column: the rail and the sidebar in dark slate whatever the
   palette, done by giving that column its own values for the tokens the
   kit paints it with. */
[data-rail], [data-sidebar] {
  --color-surface: #1b2a3a;
  --color-surface-raised: #26394d;
  --color-surface-overlay: #223548;
  --color-border: #2c4157;
  --color-border-strong: #3d5670;
  --color-ink: #e6eef8;
  --color-ink-muted: #a7b8cc;
  --color-ink-subtle: #8ea2b9;
  --color-ink-disabled: #52657a;
  --color-accent: #7fd4ff;
  --color-accent-hover: #a5e1ff;
  --color-accent-subtle: #24405a;
  --color-on-accent: #06192b;
  --color-selection: #24405a;
  --color-danger: #f57c6f;
  --color-danger-hover: #f59185;
  --color-danger-subtle: #3d2325;
  --color-on-danger: #290f0c;
}`
	shadows := map[string]string{}
	for k, v := range base.Spec.Shadows {
		shadows[k] = v
	}
	return Example{
		Key:         "constellation",
		Name:        "Constellation",
		Description: "Deep-Tech with a network of points and lines drawn live behind everything, never the same twice, and teal after dark.",
		Spec: Spec{
			Colors:  Palette{Light: light, Dark: dark},
			Shape:   Shape{RadiusControl: intPtr(8), RadiusOverlay: intPtr(12)},
			Shadows: shadows,
			Effect:  "constellation",
			CSS:     css,
		},
	}
}

// deepTech is cool slate and glass: a grey-blue gradient by day, navy by night,
// one cyan accent for what is current, and cards that float on the backdrop.
func deepTech() Example {
	light := map[string]string{
		"canvas":                 "#e9eef4",
		"surface":                "#f7f9fc",
		"surface-raised":         "#e3e9f1",
		"surface-sunken":         "#d9e1ea",
		"surface-overlay":        "#ffffff",
		"surface-glass":          "rgb(247 249 252 / 0.72)",
		"backdrop-from":          "#f2f5f9",
		"backdrop-to":            "#c6d3e1",
		"border":                 "#cfd9e5",
		"border-strong":          "#a9b8c9",
		"ink":                    "#17222f",
		"ink-muted":              "#4f6178",
		"ink-subtle":             "#52657b",
		"ink-disabled":           "#aab8c7",
		"primary":                "#17222f",
		"primary-hover":          "#2a3a4e",
		"on-primary":             "#f7f9fc",
		"accent":                 "#1f5fc5",
		"accent-hover":           "#245cb8",
		"accent-subtle":          "#dfe9fb",
		"on-accent":              "#ffffff",
		"focus":                  "#1f5fc5",
		"selection":              "#d6e3fa",
		"danger":                 "#b6342a",
		"danger-hover":           "#a83a30",
		"danger-subtle":          "#fae4e1",
		"on-danger":              "#ffffff",
		"success":                "#007156",
		"success-subtle":         "#dff3ec",
		"warning":                "#c98a1e",
		"warning-subtle":         "#fbefd6",
		"epic":                   "#6f56c5",
		"status-todo":            "#7b8fa6",
		"status-progress":        "#c98a1e",
		"status-done":            "#1f9d7a",
		"status-todo-subtle":     "#e8edf3",
		"status-progress-subtle": "#fbefd6",
		"status-done-subtle":     "#dff3ec",
		"chart-1":                "#1f5fc5",
		"chart-2":                "#007156",
		"chart-3":                "#6a50bf",
		"chart-4":                "#895a00",
		"chart-5":                "#006e7e",
		"chart-6":                "#b6342a",
	}
	dark := map[string]string{
		"canvas":                 "#0d1826",
		"surface":                "#15243a",
		"surface-raised":         "#1c2f4a",
		"surface-sunken":         "#0a1320",
		"surface-overlay":        "#1a2c45",
		"surface-glass":          "rgb(21 36 58 / 0.62)",
		"backdrop-from":          "#16294a",
		"backdrop-to":            "#0a1421",
		"border":                 "#274060",
		"border-strong":          "#3a5a82",
		"ink":                    "#e6eef8",
		"ink-muted":              "#a7b8cc",
		"ink-subtle":             "#8397ae",
		"ink-disabled":           "#4f6178",
		"primary":                "#5cc8ff",
		"primary-hover":          "#7fd4ff",
		"on-primary":             "#06192b",
		"accent":                 "#5cc8ff",
		"accent-hover":           "#8ad9ff",
		"accent-subtle":          "#173552",
		"on-accent":              "#06192b",
		"focus":                  "#5cc8ff",
		"selection":              "#1f4266",
		"danger":                 "#f0776b",
		"danger-hover":           "#f59185",
		"danger-subtle":          "#3d2325",
		"on-danger":              "#290f0c",
		"success":                "#48d0b0",
		"success-subtle":         "#163a35",
		"warning":                "#f2b85c",
		"warning-subtle":         "#3c2f16",
		"epic":                   "#b39cf0",
		"status-todo":            "#7b8fa6",
		"status-progress":        "#f2b85c",
		"status-done":            "#48d0b0",
		"status-todo-subtle":     "#1c2a3d",
		"status-progress-subtle": "#3c2f16",
		"status-done-subtle":     "#163a35",
		"chart-1":                "#5cc8ff",
		"chart-2":                "#48d0b0",
		"chart-3":                "#b39cf0",
		"chart-4":                "#f2b85c",
		"chart-5":                "#3fb9d6",
		"chart-6":                "#f0776b",
	}
	// The backdrop is drawn, not loaded: a glow in one corner and a faint
	// lattice of points, so the theme carries no file.
	css := `.bg-backdrop {
  background-color: var(--color-backdrop-to);
  background-image:
    radial-gradient(circle at 18% 12%, var(--color-backdrop-from) 0, transparent 46%),
    radial-gradient(circle at 82% 88%, var(--color-accent-subtle) 0, transparent 40%),
    radial-gradient(var(--color-border-strong) 0.8px, transparent 1px);
  background-size: auto, auto, 28px 28px;
  background-attachment: fixed;
}`
	return Example{
		Key:         "deep-tech",
		Name:        "Deep-Tech",
		Description: "Slate and glass: a grey-blue gradient by day, navy by night, one cyan accent.",
		Spec: Spec{
			Colors: Palette{Light: light, Dark: dark},
			Shape:  Shape{RadiusControl: intPtr(8), RadiusOverlay: intPtr(12)},
			Shadows: map[string]string{
				"1": "0 1px 2px rgb(10 20 33 / 0.10)",
				"2": "0 10px 28px rgb(10 20 33 / 0.22)",
				"3": "0 18px 44px rgb(10 20 33 / 0.24), inset 0 1px 0 rgb(255 255 255 / 0.08)",
			},
			CSS: css,
		},
	}
}
