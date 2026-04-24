// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Integration presets for MST-specific webhook providers.
//
// The generic webhook framework in services/integrations is provider-agnostic
// — it just accepts signed JSON/query-string payloads and renders them to
// messages via a Go text/template. Presets are a frontend convenience that
// pre-fills the admin creation form with sensible defaults for each known
// provider so the admin does not need to re-derive the template every time.
//
// Adding a new preset:
//   1. Append a new entry below with a unique `id`.
//   2. The `config` object is stored on the Connector as JSONB and read back
//      by the webhook handler (see services/integrations/internal/model.ConnectorConfig).
//   3. `defaultTemplate` becomes the pre-filled template in the Create Route
//      modal — admin can still edit per-route before saving.
//   4. Document the preset in docs/mst-integrations.md.
//
// Field meanings:
//   - `httpMethod`         — which HTTP verb the provider sends (POST default, GET for Keitaro)
//   - `signatureLocation`  — "header" (default) or "query" (Keitaro)
//   - `signatureParamName` — name of the header or query parameter carrying the HMAC signature
//   - `timestampParamName` — name of the header or query parameter carrying the UNIX timestamp
//
// Backend defaults (if config is empty/missing):
//   httpMethod=POST, signatureLocation=header,
//   signatureParamName=X-Orbit-Signature, timestampParamName=X-Orbit-Timestamp

export type IntegrationPresetConfig = {
  preset_id: string;
  http_method: 'POST' | 'GET';
  signature_location: 'header' | 'query';
  signature_param_name: string;
  timestamp_param_name: string;
};

export type IntegrationPreset = {
  id: string;
  /** Stored in Connector.Type — free-form tag used only for UI grouping. */
  type: string;
  /** Shown in the create-form dropdown. */
  displayName: string;
  /** Short explanation shown under the dropdown when selected. */
  description: string;
  /** Pre-fill for Connector.DisplayName — admin can edit before saving. */
  defaultConnectorDisplayName: string;
  /** Pre-fill for Route.Template — Go text/template syntax with dot-notation. */
  defaultTemplate: string;
  /** Pre-fill for Route.EventFilter — comma-separated event type list. */
  defaultEventFilter?: string;
  /** Optional allowed event types — enables the chip-select UI in route form. */
  availableEventTypes?: string[];
  /** Persisted into Connector.Config (JSONB). Read by the webhook handler. */
  config: IntegrationPresetConfig;
  /** Rendered as multi-line admin instructions under the dropdown. */
  adminInstructions: string;
  /** Provider status — only "ready" means we have verified the format live. */
  status: 'ready' | 'framework-only';
};

export const INTEGRATION_PRESETS: IntegrationPreset[] = [
  {
    id: 'saturn_deploy',
    type: 'inbound_webhook',
    displayName: 'Saturn.ac — Deploy status',
    description: 'Deployment events from our own Saturn.ac PaaS into #dev.',
    defaultConnectorDisplayName: 'Saturn.ac Deploy',
    defaultTemplate: '🚀 Deploy {{.service}} → {{.status}} ({{.commit_sha}}) by {{.user}}',
    defaultEventFilter: 'deploy.started,deploy.succeeded,deploy.failed',
    availableEventTypes: ['deploy.started', 'deploy.succeeded', 'deploy.failed'],
    config: {
      preset_id: 'saturn_deploy',
      http_method: 'POST',
      signature_location: 'header',
      signature_param_name: 'X-Orbit-Signature',
      timestamp_param_name: 'X-Orbit-Timestamp',
    },
    adminInstructions: [
      '1. Save this connector to get a secret.',
      '2. In Saturn.ac dashboard → Webhooks → Add webhook, paste the URL shown below.',
      '3. Set HMAC secret to the secret from step 1.',
      '4. Enable events: deploy.started, deploy.succeeded, deploy.failed.',
      '5. Create a route pointing to the #dev channel.',
    ].join('\n'),
    status: 'ready',
  },
  {
    id: 'insightflow',
    type: 'inbound_webhook',
    displayName: 'InsightFlow — Conversions',
    description: 'Conversion events from InsightFlow tracker into #alerts.',
    defaultConnectorDisplayName: 'InsightFlow Alerts',
    defaultTemplate: '💰 Конверсия: {{.offer_name}} — {{.amount}} {{.currency}}',
    defaultEventFilter: 'conversion.lead,conversion.sale',
    availableEventTypes: ['conversion.lead', 'conversion.sale', 'conversion.chargeback'],
    config: {
      preset_id: 'insightflow',
      http_method: 'POST',
      signature_location: 'header',
      signature_param_name: 'X-InsightFlow-Signature',
      timestamp_param_name: 'X-InsightFlow-Timestamp',
    },
    adminInstructions: [
      '1. Save this connector to get a secret.',
      '2. In InsightFlow → Settings → Webhooks → New webhook.',
      '3. Paste the URL below and set the HMAC secret.',
      '4. Adjust the template above to match real InsightFlow payload fields',
      '   (send a test webhook first and inspect the delivery log for field names).',
      '5. Route to your #alerts channel.',
      '',
      'Note: framework ready, pending live testing with real InsightFlow credentials.',
    ].join('\n'),
    status: 'framework-only',
  },
  {
    id: 'asa_analytics',
    type: 'inbound_webhook',
    displayName: 'ASA Analytics — Campaign alerts',
    description: 'Apple Search Ads campaign events into #marketing.',
    defaultConnectorDisplayName: 'ASA Analytics',
    defaultTemplate: '📊 Кампания {{.campaign_name}}: CPI ${{.cpi}}, {{.installs}} установок, ${{.spend}} потрачено',
    defaultEventFilter: 'campaign.alert,campaign.limit_reached',
    availableEventTypes: ['campaign.alert', 'campaign.limit_reached', 'campaign.paused'],
    config: {
      preset_id: 'asa_analytics',
      http_method: 'POST',
      signature_location: 'header',
      signature_param_name: 'X-ASA-Signature',
      timestamp_param_name: 'X-ASA-Timestamp',
    },
    adminInstructions: [
      '1. Save this connector to get a secret.',
      '2. In Apple Search Ads → Campaign settings → Webhooks.',
      '3. Paste the URL below, set the HMAC secret.',
      '4. Verify template fields match your real ASA payload format.',
      '5. Route to #marketing or another campaign channel.',
      '',
      'Note: framework ready, pending live testing with real ASA credentials.',
    ].join('\n'),
    status: 'framework-only',
  },
  {
    id: 'keitaro',
    type: 'inbound_webhook',
    displayName: 'Keitaro — Postbacks',
    description: 'GET-based postbacks from Keitaro tracker (HMAC in query).',
    defaultConnectorDisplayName: 'Keitaro Postbacks',
    defaultTemplate: '🎯 Postback: {{.campaign}} — {{.status}} ({{.payout}}$)',
    defaultEventFilter: 'postback.approved,postback.rejected',
    availableEventTypes: ['postback.approved', 'postback.rejected', 'postback.pending'],
    config: {
      preset_id: 'keitaro',
      http_method: 'GET',
      signature_location: 'query',
      signature_param_name: 'sign',
      timestamp_param_name: 'ts',
    },
    adminInstructions: [
      '1. Save this connector to get a secret.',
      '2. In Keitaro → Tracker → Postback URLs → New postback.',
      '3. Paste the URL below and append Keitaro tokens:',
      '   ?campaign={campaign_name}&status={status}&payout={payout}&ts={unix}&sign={hmac}',
      '4. Keitaro signs with HMAC-SHA256(ts + "." + payload_without_sign) using your secret.',
      '5. Route to your traffic desk channel.',
      '',
      'Note: framework ready, pending live testing with real Keitaro credentials.',
    ].join('\n'),
    status: 'framework-only',
  },
  {
    id: 'alertmanager',
    type: 'inbound_webhook',
    displayName: 'Prometheus Alertmanager',
    description: 'Alerts from Prometheus Alertmanager into #monitoring channel.',
    defaultConnectorDisplayName: 'MST Monitoring',
    defaultTemplate: '{{if eq .status "firing"}}🔴{{else}}✅{{end}} [{{.status | toUpper}}] {{.commonLabels.alertname}}\nСервис: {{if .commonLabels.service}}{{.commonLabels.service}}{{else}}{{.commonLabels.instance}}{{end}}\n{{.commonAnnotations.description}}',
    defaultEventFilter: 'alert.firing,alert.resolved',
    availableEventTypes: ['alert.firing', 'alert.resolved'],
    config: {
      preset_id: 'alertmanager',
      http_method: 'POST',
      signature_location: 'header',
      signature_param_name: 'Authorization',
      timestamp_param_name: '',
    },
    adminInstructions: [
      '1. Save this connector to get a secret token.',
      '2. In your environment set:',
      '   ALERTMANAGER_WEBHOOK_URL=<Webhook URL shown after saving>',
      '   ALERTMANAGER_WEBHOOK_SECRET=<secret token from step 1>',
      '3. Restart Alertmanager: docker compose restart alertmanager',
      '   Alertmanager sends the token as: Authorization: Bearer <secret>',
      '4. Create a route pointing to your #monitoring channel.',
      '',
      'Alertmanager sends POST with Prometheus alert payload.',
      'Template fields: .status, .commonLabels.alertname, .commonLabels.service,',
      '.commonAnnotations.summary, .commonAnnotations.description',
    ].join('\n'),
     status: 'ready',
   },
   {
     id: 'generic',
    type: 'inbound_webhook',
    displayName: 'Generic webhook',
    description: 'Manual configuration for any provider not listed above.',
    defaultConnectorDisplayName: '',
    defaultTemplate: '{{.event}}: {{.message}}',
    config: {
      preset_id: 'generic',
      http_method: 'POST',
      signature_location: 'header',
      signature_param_name: 'X-Orbit-Signature',
      timestamp_param_name: 'X-Orbit-Timestamp',
    },
    adminInstructions: [
      '1. Save this connector to get a secret.',
      '2. Configure your provider to POST JSON to the URL below.',
      '3. Provider must sign the request:',
      '   X-Orbit-Signature: HMAC-SHA256(timestamp + "." + body)',
      '   X-Orbit-Timestamp: current UNIX seconds',
      '4. Customize the template to match your payload shape.',
    ].join('\n'),
    status: 'ready',
  },
];

export function findPresetById(id: string | undefined): IntegrationPreset | undefined {
  if (!id) return undefined;
  return INTEGRATION_PRESETS.find((p) => p.id === id);
}

export function getDefaultPreset(): IntegrationPreset {
  return INTEGRATION_PRESETS[0];
}
