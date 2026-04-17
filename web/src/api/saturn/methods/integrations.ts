import { request } from '../client';
import type {
  SaturnConnectorCreateResponse,
  SaturnIntegrationConnector,
  SaturnIntegrationDelivery,
  SaturnIntegrationRoute,
} from '../types';

export async function fetchConnectors(limit = 50, offset = 0) {
  return request<{ data: SaturnIntegrationConnector[]; total: number }>(
    'GET',
    `/integrations/connectors?limit=${limit}&offset=${offset}`,
  );
}

export async function fetchConnector(connectorId: string) {
  return request<SaturnIntegrationConnector>('GET', `/integrations/connectors/${connectorId}`);
}

export async function createConnector(data: { name: string; display_name: string; type: string; bot_id?: string }) {
  return request<SaturnConnectorCreateResponse>('POST', '/integrations/connectors', data);
}

export async function updateConnector(
  connectorId: string,
  data: Partial<{ display_name: string; is_active: boolean; config: Record<string, unknown> }>,
) {
  return request<SaturnIntegrationConnector>('PATCH', `/integrations/connectors/${connectorId}`, data);
}

export async function deleteConnector(connectorId: string) {
  return request<void>('DELETE', `/integrations/connectors/${connectorId}`);
}

export async function rotateConnectorSecret(connectorId: string) {
  return request<{ secret: string }>('POST', `/integrations/connectors/${connectorId}/rotate-secret`);
}

export async function fetchRoutes(connectorId: string) {
  const result = await request<{ data: SaturnIntegrationRoute[] }>('GET', `/integrations/connectors/${connectorId}/routes`);
  return result.data;
}

export async function createRoute(connectorId: string, data: { chat_id: string; event_filter?: string; template?: string }) {
  return request<SaturnIntegrationRoute>('POST', `/integrations/connectors/${connectorId}/routes`, data);
}

export async function deleteRoute(routeId: string) {
  return request<void>('DELETE', `/integrations/routes/${routeId}`);
}

export async function updateRoute(
  routeId: string,
  data: Partial<{ event_filter: string; template: string; is_active: boolean }>,
) {
  return request<SaturnIntegrationRoute>('PATCH', `/integrations/routes/${routeId}`, data);
}

export interface SaturnConnectorStats {
  window: string;
  total: number;
  delivered: number;
  failed: number;
  pending: number;
  dead_letter: number;
  last_delivery_at?: string;
}

export async function fetchConnectorStats(connectorId: string, window = '24h') {
  return request<SaturnConnectorStats>(
    'GET',
    `/integrations/connectors/${connectorId}/stats?window=${encodeURIComponent(window)}`,
  );
}

export interface SaturnTestConnectorResult {
  delivery_ids: string[];
  route_count: number;
  event_type: string;
}

export async function testConnector(
  connectorId: string,
  data: { event_type?: string; payload?: Record<string, unknown> } = {},
) {
  return request<SaturnTestConnectorResult>('POST', `/integrations/connectors/${connectorId}/test`, data);
}

export async function previewTemplate(data: {
  connector_id?: string;
  template: string;
  event_type?: string;
  sample_payload?: Record<string, unknown>;
}) {
  return request<{ rendered: string }>('POST', '/integrations/templates/preview', data);
}

export async function fetchDeliveries(connectorId: string, limit = 50, offset = 0, status?: string) {
  let url = `/integrations/connectors/${connectorId}/deliveries?limit=${limit}&offset=${offset}`;
  if (status) url += `&status=${status}`;
  return request<{ data: SaturnIntegrationDelivery[]; total: number }>('GET', url);
}

export async function retryDelivery(deliveryId: string) {
  return request<void>('POST', `/integrations/deliveries/${deliveryId}/retry`);
}
