import { memo, useEffect, useState } from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type {
  SaturnConnectorCreateResponse,
  SaturnIntegrationConnector,
  SaturnIntegrationDelivery,
  SaturnIntegrationRoute,
} from '../../../api/saturn/types';
import type { SaturnConnectorStats } from '../../../api/saturn/methods/integrations';

import { copyTextToClipboard } from '../../../util/clipboard';

import useFlag from '../../../hooks/useFlag';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Button from '../../ui/Button';
import ConfirmDialog from '../../ui/ConfirmDialog';
import InputText from '../../ui/InputText';
import ListItem from '../../ui/ListItem';
import Modal from '../../ui/Modal';
import Select from '../../ui/Select';
import Spinner from '../../ui/Spinner';

import {
  createConnector,
  createRoute,
  deleteConnector,
  deleteRoute,
  fetchConnectorStats,
  fetchConnectors,
  fetchDeliveries,
  fetchRoutes,
  previewTemplate,
  retryDelivery,
  rotateConnectorSecret,
  testConnector,
  updateConnector,
} from '../../../api/saturn/methods/integrations';
import { request as saturnRequest } from '../../../api/saturn/client';
import {
  findPresetById,
  getDefaultPreset,
  INTEGRATION_PRESETS,
} from '../../../api/saturn/presets/integrations';

type ViewMode = 'list' | 'edit' | 'routes';

const SettingsIntegrations = () => {
  const { showNotification } = getActions();
  const lang = useLang();

  const [connectors, setConnectors] = useState<SaturnIntegrationConnector[]>([]);
  const [isLoading, markLoading, unmarkLoading] = useFlag(false);
  const [viewMode, setViewMode] = useState<ViewMode>('list');
  const [selectedConnector, setSelectedConnector] = useState<SaturnIntegrationConnector | undefined>();
  const [connectorSecret, setConnectorSecret] = useState<string | undefined>();
  const [routes, setRoutes] = useState<SaturnIntegrationRoute[]>([]);
  const [deliveries, setDeliveries] = useState<SaturnIntegrationDelivery[]>([]);

  // Create form
  const [isCreateOpen, openCreate, closeCreate] = useFlag(false);
  const [isDeleteOpen, openDelete, closeDelete] = useFlag(false);
  const [deletingId, setDeletingId] = useState<string | undefined>();
  const [newName, setNewName] = useState('');
  const [newDisplayName, setNewDisplayName] = useState('');
  const [newPresetId, setNewPresetId] = useState<string>(getDefaultPreset().id);

  // Stats
  const [stats, setStats] = useState<SaturnConnectorStats | undefined>();

  // Delivery detail modal
  const [deliveryDetail, setDeliveryDetail] = useState<SaturnIntegrationDelivery | undefined>();
  const [isDeliveryOpen, openDelivery, closeDelivery] = useFlag(false);

  // Route form
  const [isRouteCreateOpen, openRouteCreate, closeRouteCreate] = useFlag(false);
  const [newRouteChatId, setNewRouteChatId] = useState('');
  const [newRouteEventFilter, setNewRouteEventFilter] = useState('');
  const [newRouteTemplate, setNewRouteTemplate] = useState('');
  const [templatePreview, setTemplatePreview] = useState<string | undefined>();

  const loadConnectors = useLastCallback(async () => {
    markLoading();
    try {
      const result = await fetchConnectors();
      if (result?.data) {
        setConnectors(result.data);
      }
    } finally {
      unmarkLoading();
    }
  });

  useEffect(() => {
    loadConnectors();
  }, [loadConnectors]);

  const handleCreate = useLastCallback(async () => {
    if (!newName.trim() || !newDisplayName.trim()) return;
    const preset = findPresetById(newPresetId) ?? getDefaultPreset();
    try {
      const result = await createConnector({
        name: newName.trim(),
        display_name: newDisplayName.trim(),
        type: preset.type,
      }) as SaturnConnectorCreateResponse;

      if (!result?.connector) return;

      // Persist preset config via follow-up PATCH — createConnector endpoint
      // does not accept config, but updateConnector does.
      let connectorWithConfig = result.connector;
      try {
        const updated = await updateConnector(result.connector.id, {
          config: preset.config as unknown as Record<string, unknown>,
        });
        if (updated) connectorWithConfig = updated;
      } catch {
        // Non-fatal — connector is usable without preset config, admin can edit later.
      }

      setSelectedConnector(connectorWithConfig);
      setConnectorSecret(result.secret);
      setViewMode('edit');
      showNotification({ message: lang('ConnectorCreated') });
      closeCreate();
      setNewName('');
      setNewDisplayName('');
      setNewPresetId(getDefaultPreset().id);
      loadConnectors();
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const handlePresetChange = useLastCallback((e: { target: HTMLSelectElement }) => {
    const presetId = e.target.value;
    setNewPresetId(presetId);
    const preset = findPresetById(presetId);
    if (preset && !newDisplayName.trim()) {
      setNewDisplayName(preset.defaultConnectorDisplayName);
    }
  });

  const handleDelete = useLastCallback(async () => {
    if (!deletingId) return;
    try {
      await deleteConnector(deletingId);
      showNotification({ message: lang('ConnectorDeleted') });
      closeDelete();
      setDeletingId(undefined);
      if (selectedConnector?.id === deletingId) {
        setViewMode('list');
        setSelectedConnector(undefined);
      }
      loadConnectors();
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const handleRotateSecret = useLastCallback(async () => {
    if (!selectedConnector) return;
    try {
      const result = await rotateConnectorSecret(selectedConnector.id);
      if (result?.secret) {
        setConnectorSecret(result.secret);
        showNotification({ message: lang('SecretRotated') });
      }
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const loadStats = useLastCallback(async (connectorId: string) => {
    try {
      const result = await fetchConnectorStats(connectorId, '24h');
      setStats(result);
    } catch {
      setStats(undefined);
    }
  });

  // Pull 24h stats whenever the edit view opens a connector.
  useEffect(() => {
    if (viewMode === 'edit' && selectedConnector) {
      loadStats(selectedConnector.id);
    } else if (viewMode === 'list') {
      setStats(undefined);
    }
  }, [viewMode, selectedConnector, loadStats]);

  const handleTest = useLastCallback(async () => {
    if (!selectedConnector) return;
    try {
      const result = await testConnector(selectedConnector.id);
      showNotification({
        message: `Test fired: ${result.delivery_ids.length} delivery(ies) across ${result.route_count} route(s)`,
      });
      // Stats reflect the fresh delivery.
      loadStats(selectedConnector.id);
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const handleOpenDelivery = useLastCallback(async (deliveryId: string) => {
    try {
      const detail = await saturnRequest<SaturnIntegrationDelivery>('GET', `/integrations/deliveries/${deliveryId}`);
      setDeliveryDetail(detail);
      openDelivery();
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const handleViewRoutes = useLastCallback(async (connector: SaturnIntegrationConnector) => {
    setSelectedConnector(connector);
    setViewMode('routes');
    try {
      const [routesResult, deliveriesResult] = await Promise.all([
        fetchRoutes(connector.id),
        fetchDeliveries(connector.id),
      ]);
      setRoutes(routesResult || []);
      setDeliveries(deliveriesResult?.data || []);
    } catch {
      // Non-critical
    }
  });

  const handleOpenRouteCreate = useLastCallback(() => {
    // Pre-fill template + event_filter from the connector's preset (if any).
    const presetId = (selectedConnector?.config as { preset_id?: string } | undefined)?.preset_id;
    const preset = findPresetById(presetId);
    if (preset) {
      if (!newRouteTemplate) setNewRouteTemplate(preset.defaultTemplate);
      if (!newRouteEventFilter && preset.defaultEventFilter) {
        setNewRouteEventFilter(preset.defaultEventFilter);
      }
    }
    openRouteCreate();
  });

  const handlePreviewTemplate = useLastCallback(async () => {
    if (!selectedConnector || !newRouteTemplate.trim()) {
      setTemplatePreview(undefined);
      return;
    }
    try {
      const result = await previewTemplate({
        connector_id: selectedConnector.id,
        template: newRouteTemplate,
        event_type: newRouteEventFilter.split(',')[0]?.trim() || 'preview.event',
        sample_payload: {
          service: 'orbit-web',
          status: 'succeeded',
          commit_sha: 'abc1234',
          user: 'admin',
          message: 'Sample deploy payload',
          campaign_name: 'Spring 2026',
          offer_name: 'Demo offer',
          amount: 1234,
          currency: 'USD',
        },
      });
      setTemplatePreview(result?.rendered ?? '');
    } catch (e) {
      setTemplatePreview(`Error: ${e}`);
    }
  });

  const handleCreateRoute = useLastCallback(async () => {
    if (!selectedConnector || !newRouteChatId.trim()) return;
    try {
      await createRoute(selectedConnector.id, {
        chat_id: newRouteChatId.trim(),
        event_filter: newRouteEventFilter.trim() || undefined,
        template: newRouteTemplate.trim() || undefined,
      });
      closeRouteCreate();
      setNewRouteChatId('');
      setNewRouteEventFilter('');
      setNewRouteTemplate('');
      handleViewRoutes(selectedConnector);
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const handleDeleteRoute = useLastCallback(async (routeId: string) => {
    try {
      await deleteRoute(routeId);
      if (selectedConnector) {
        handleViewRoutes(selectedConnector);
      }
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const handleRetryDelivery = useLastCallback(async (deliveryId: string) => {
    try {
      await retryDelivery(deliveryId);
      showNotification({ message: 'Retry queued' });
      if (selectedConnector) {
        handleViewRoutes(selectedConnector);
      }
    } catch (e) {
      showNotification({ message: String(e) });
    }
  });

  const handleCopySecret = useLastCallback((secret: string) => {
    copyTextToClipboard(secret);
    // @ts-expect-error TODO(phase-8D-cleanup): ExactTextCopied lang signature mismatch
    showNotification({ message: lang('ExactTextCopied', secret.substring(0, 20) + '...') });
  });

  const handleConfirmDelete = useLastCallback((id: string) => {
    setDeletingId(id);
    openDelete();
  });

  const handleBack = useLastCallback(() => {
    setViewMode('list');
    setSelectedConnector(undefined);
    setConnectorSecret(undefined);
  });

  // Edit view
  if (viewMode === 'edit' && selectedConnector) {
    const editedPresetId = (selectedConnector.config as { preset_id?: string } | undefined)?.preset_id;
    const editedPreset = findPresetById(editedPresetId);
    // Webhook URL is served by the integrations service behind the gateway.
    // On localhost the web dev server lives on :3000 but the webhook is on :8080;
    // on Saturn both share the same origin.
    const gatewayOrigin = window.location.port === '3000'
      ? window.location.origin.replace(':3000', ':8080')
      : window.location.origin;
    const webhookUrl = `${gatewayOrigin}/api/v1/webhooks/in/${selectedConnector.id}`;

    return (
      <div className="settings-content custom-scroll">
        <div className="settings-item">
          <h4>{selectedConnector.display_name}</h4>
          <p className="settings-item-description">{lang('ConnectorName')}: {selectedConnector.name}</p>
          <p className="settings-item-description">{lang('ConnectorType')}: {selectedConnector.type}</p>
          {editedPreset && (
            <p className="settings-item-description">
              Preset: {editedPreset.displayName}
              {editedPreset.status === 'framework-only' ? ' — framework only, pending live testing' : ''}
            </p>
          )}
        </div>

        {stats && (
          <div className="settings-item">
            <div className="settings-item-header">24h stats</div>
            <p className="settings-item-description">
              Total: <b>{stats.total}</b> · Delivered: <b>{stats.delivered}</b> ·
              {' '}Failed: <b>{stats.failed}</b> · Dead-letter: <b>{stats.dead_letter}</b>
            </p>
            {stats.last_delivery_at && (
              <p className="settings-item-description">
                Last delivery: {new Date(stats.last_delivery_at).toLocaleString()}
              </p>
            )}
          </div>
        )}

        <div className="settings-item">
          <div className="settings-item-header">Inbound webhook URL</div>
          <code style="word-break: break-all; font-size: 0.75rem">{webhookUrl}</code>
          <Button size="smaller" onClick={() => handleCopySecret(webhookUrl)}>Copy URL</Button>
        </div>

        {connectorSecret && (
          <div className="settings-item">
            <div className="settings-item-header">{lang('ConnectorSecret')}</div>
            <code style="word-break: break-all; font-size: 0.75rem">{connectorSecret}</code>
            <Button size="smaller" onClick={() => handleCopySecret(connectorSecret)}>Copy</Button>
          </div>
        )}

        <div className="settings-item-footer">
          <Button onClick={handleTest} color="primary">Send test event</Button>
          <Button onClick={handleRotateSecret} color="translucent">
            {lang('SecretRotated')}
          </Button>
          <Button onClick={() => handleViewRoutes(selectedConnector)}>
            {lang('Routes')}
          </Button>
          <Button onClick={() => handleConfirmDelete(selectedConnector.id)} color="danger">
            {lang('DeleteConnector')}
          </Button>
          <Button onClick={handleBack} color="translucent">
            {lang('Back')}
          </Button>
        </div>
      </div>
    );
  }

  // Routes view
  if (viewMode === 'routes' && selectedConnector) {
    return (
      <div className="settings-content custom-scroll">
        <div className="settings-item">
          <h4>{selectedConnector.display_name} — {lang('Routes')}</h4>
          <Button onClick={handleOpenRouteCreate} color="primary" size="smaller">
            {lang('CreateRoute')}
          </Button>
        </div>

        <div className="settings-main-menu">
          {routes.map((route) => (
            <ListItem
              key={route.id}
              narrow
              contextActions={[{
                title: lang('DeleteRoute'),
                icon: 'delete',
                handler: () => handleDeleteRoute(route.id),
                destructive: true,
              }]}
            >
              <span className="title">Chat: {route.chat_id}</span>
              {route.event_filter && <span className="subtitle">Filter: {route.event_filter}</span>}
            </ListItem>
          ))}
          {!routes.length && <p className="settings-item-description">No routes configured</p>}
        </div>

        <div className="settings-item">
          <h4>{lang('Deliveries')}</h4>
        </div>
        <div className="settings-main-menu">
          {deliveries.slice(0, 20).map((delivery) => (
            <ListItem
              key={delivery.id}
              narrow
              secondaryIcon="next"
              onClick={() => handleOpenDelivery(delivery.id)}
              contextActions={delivery.status === 'failed' ? [{
                title: lang('RetryDelivery'),
                icon: 'replace',
                handler: () => handleRetryDelivery(delivery.id),
              }] : undefined}
            >
              <span className="title">{delivery.event_type || 'event'}</span>
              <span className="subtitle">
                {delivery.status} · attempt {delivery.attempt_count}/3 · {new Date(delivery.created_at).toLocaleString()}
              </span>
            </ListItem>
          ))}
          {!deliveries.length && <p className="settings-item-description">No deliveries yet</p>}
        </div>

        <div className="settings-item">
          <Button onClick={handleBack} color="translucent">{lang('Back')}</Button>
        </div>

        <Modal isOpen={isRouteCreateOpen} onClose={closeRouteCreate} title={lang('CreateRoute')}>
          <div className="settings-item">
            <InputText
              label={lang('TargetChat')}
              value={newRouteChatId}
              onChange={(e) => setNewRouteChatId((e.target as HTMLInputElement).value)}
            />
            {(() => {
              const presetId = (selectedConnector?.config as { preset_id?: string } | undefined)?.preset_id;
              const preset = findPresetById(presetId);
              const selected = new Set(
                newRouteEventFilter.split(',').map((s) => s.trim()).filter(Boolean),
              );
              if (preset?.availableEventTypes?.length) {
                const toggle = (ev: string) => {
                  const next = new Set(selected);
                  if (next.has(ev)) next.delete(ev); else next.add(ev);
                  setNewRouteEventFilter(Array.from(next).join(','));
                };
                return (
                  <div className="settings-item">
                    <div className="settings-item-header">{lang('EventFilter')}</div>
                    <div style="display: flex; flex-wrap: wrap; gap: 0.5rem">
                      {preset.availableEventTypes.map((ev) => (
                        <Button
                          key={ev}
                          size="tiny"
                          color={selected.has(ev) ? 'primary' : 'translucent'}
                          onClick={() => toggle(ev)}
                        >
                          {ev}
                        </Button>
                      ))}
                    </div>
                  </div>
                );
              }
              return (
                <InputText
                  label={lang('EventFilter')}
                  value={newRouteEventFilter}
                  onChange={(e) => setNewRouteEventFilter((e.target as HTMLInputElement).value)}
                />
              );
            })()}
            <InputText
              label={lang('MessageTemplate')}
              value={newRouteTemplate}
              onChange={(e) => setNewRouteTemplate((e.target as HTMLInputElement).value)}
            />
            <Button size="smaller" color="translucent" onClick={handlePreviewTemplate}>
              Preview template
            </Button>
            {templatePreview !== undefined && (
              <div className="settings-item">
                <div className="settings-item-header">Rendered preview</div>
                <p className="settings-item-description" style="white-space: pre-wrap; word-break: break-word">
                  {templatePreview || '(empty)'}
                </p>
              </div>
            )}
            <Button onClick={handleCreateRoute} color="primary">
              {lang('CreateRoute')}
            </Button>
          </div>
        </Modal>

        <Modal
          isOpen={isDeliveryOpen}
          onClose={closeDelivery}
          title={deliveryDetail ? `Delivery: ${deliveryDetail.event_type || 'event'}` : 'Delivery'}
        >
          {deliveryDetail && (
            <div className="settings-item">
              <p className="settings-item-description">
                Status: <b>{deliveryDetail.status}</b>
                {'  '}·{'  '}Attempts: <b>{deliveryDetail.attempt_count}</b>
              </p>
              <p className="settings-item-description">
                Created: {new Date(deliveryDetail.created_at).toLocaleString()}
                {deliveryDetail.delivered_at && (
                  <> · Delivered: {new Date(deliveryDetail.delivered_at).toLocaleString()}</>
                )}
              </p>
              {deliveryDetail.last_error && (
                <>
                  <div className="settings-item-header">Last error</div>
                  <code style="word-break: break-all; font-size: 0.75rem">{deliveryDetail.last_error}</code>
                </>
              )}
              {deliveryDetail.attempts && deliveryDetail.attempts.length > 0 && (
                <>
                  <div className="settings-item-header">Attempts timeline</div>
                  {deliveryDetail.attempts.map((a) => (
                    <div key={a.id} className="settings-item-description">
                      #{a.attempt_no} · <b>{a.status}</b>
                      {a.response_status ? ` · HTTP ${a.response_status}` : ''}
                      {' '}· {new Date(a.ran_at).toLocaleString()}
                      {a.error && <div style="color: var(--color-error); word-break: break-all">{a.error}</div>}
                    </div>
                  ))}
                </>
              )}
              {deliveryDetail.status === 'failed' && (
                <Button size="smaller" onClick={() => { handleRetryDelivery(deliveryDetail.id); closeDelivery(); }}>
                  {lang('RetryDelivery')}
                </Button>
              )}
            </div>
          )}
        </Modal>
      </div>
    );
  }

  // List view
  return (
    <div className="settings-content custom-scroll">
      <div className="settings-item">
        <Button onClick={openCreate} color="primary" size="smaller">
          {lang('CreateConnector')}
        </Button>
      </div>

      {isLoading && <Spinner />}

      <div className="settings-main-menu">
        {connectors.map((connector) => (
          <ListItem
            key={connector.id}
            narrow
            secondaryIcon="next"
            onClick={() => {
              setSelectedConnector(connector);
              setViewMode('edit');
            }}
            contextActions={[{
              title: lang('Routes'),
              icon: 'channel',
              handler: () => handleViewRoutes(connector),
            }, {
              title: lang('DeleteConnector'),
              icon: 'delete',
              handler: () => handleConfirmDelete(connector.id),
              destructive: true,
            }]}
          >
            <span className="title">{connector.display_name}</span>
            <span className="subtitle">{connector.name} — {connector.is_active ? 'Active' : 'Inactive'}</span>
          </ListItem>
        ))}
      </div>

      <Modal isOpen={isCreateOpen} onClose={closeCreate} title={lang('CreateConnector')}>
        <div className="settings-item">
          <Select
            id="integration-preset"
            label="Тип интеграции"
            value={newPresetId}
            onChange={handlePresetChange}
            hasArrow
          >
            {INTEGRATION_PRESETS.map((preset) => (
              <option key={preset.id} value={preset.id}>
                {preset.displayName}
                {preset.status === 'framework-only' ? ' (framework only)' : ''}
              </option>
            ))}
          </Select>
          {(() => {
            const preset = findPresetById(newPresetId);
            if (!preset) return undefined;
            return (
              <p className="settings-item-description" style="white-space: pre-wrap">
                {preset.description}
                {'\n\n'}
                {preset.adminInstructions}
              </p>
            );
          })()}
          <InputText
            label={lang('ConnectorName')}
            value={newName}
            onChange={(e) => setNewName((e.target as HTMLInputElement).value)}
          />
          <InputText
            label={lang('ConnectorDisplayName')}
            value={newDisplayName}
            onChange={(e) => setNewDisplayName((e.target as HTMLInputElement).value)}
          />
          <Button onClick={handleCreate} color="primary">
            {lang('CreateConnector')}
          </Button>
        </div>
      </Modal>

      <ConfirmDialog
        isOpen={isDeleteOpen}
        onClose={closeDelete}
        confirmHandler={handleDelete}
        title={lang('DeleteConnector')}
        textParts={lang('AreYouSure')}
        confirmIsDestructive
      />
    </div>
  );
};

export default memo(SettingsIntegrations);
