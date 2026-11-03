package model

// SummaryProvider returns the provider bound to the shared AI-summary config
// (voice summaries, next-step recommendation, session titles, /btw).
//
// The binding is mandatory for new configurations: the settings page no longer
// writes an endpoint or key, so the only way to configure the summary model is
// to reference a providers row. A missing row (deleted out from under the
// config) reads as "not configured" rather than falling back to something else.
func (c AISummaryConfig) SummaryProvider() *Provider {
	if c.ProviderID == nil {
		return nil
	}
	return GetProvider(*c.ProviderID)
}

// SummaryConfigured reports whether the summary model has a usable endpoint.
func (c AISummaryConfig) SummaryConfigured() bool {
	_, _, _, ok := c.SummaryEndpoint()
	return ok
}

// SummaryEndpoint resolves the endpoint the summarizer should call.
//
//   - format is "" when it must be inferred from the URL. ok is
//     false when nothing usable is configured — a Gemini provider is deliberately
//     rejected here because the summarizer only speaks the OpenAI and Anthropic
//     wire formats.
//
// The api/format fields are a plain endpoint fallback: read only while
// provider_id is absent. The settings UI never writes them.
func (c AISummaryConfig) SummaryEndpoint() (baseURL, key, format string, ok bool) {
	if p := c.SummaryProvider(); p != nil {
		if p.BaseURL == "" {
			return "", "", "", false
		}
		switch p.Protocol {
		case ProviderProtocolAnthropic:
			return p.BaseURL, p.APIKey, "anthropic", true
		case ProviderProtocolOpenAI:
			return p.BaseURL, p.APIKey, "openai", true
		default:
			return "", "", "", false
		}
	}
	if c.API.BaseURL == "" {
		return "", "", "", false
	}
	return c.API.BaseURL, c.API.Key, c.Format, true
}

// SummaryModel returns the model id the summarizer should request. It is a
// free-form string: providers expose different catalogs and some gateways do
// not publish one at all.
func (c AISummaryConfig) SummaryModel() string {
	return c.Model
}
