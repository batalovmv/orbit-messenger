import { memo, useEffect, useState } from '../../../lib/teact/teact';
import { getActions } from '../../../global';

import type {
  SaturnConnectorCreateResponse,
  SaturnIntegrationConnector,
  SaturnIntegrationDelivery,
  SaturnIntegrationRoute,
} from '../../../api/saturn/types';

import { copyTextToClipboard } from '../../../util/clipboard';

import useFlag from '../../../hooks/useFlag';
import useLang from '../../../hooks/useLang';
import useLastCallback from '../../../hooks/useLastCallback';

import Button from '../../ui/Button';
import ConfirmDialog from '../../ui/ConfirmDialog';
import InputText from '../../ui/InputText';
import ListItem from '../../ui/ListItem';
import Modal from '../../ui/Modal';
import Spinner from '../../ui/Spinner';

import {
  createConnector,
  createRoute,
  deleteConnector,
  deleteRoute,
  fetchConnectors,
  fetchDeliveries,
  fetchRoutes,
  retryDelivery,
  rotateConnectorSecret,
} from '../../../api/saturn/methods/integrations';

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

  // Route form
  const [isRouteCreateOpen, openRouteCreate, closeRouteCreate] = useFlag(false);
  const [newRouteChatId, setNewRouteChatId] = useState('');
  const [newRouteEventFilter, setNewRouteEventFilter] = useState('');
  const [newRouteTemplate, setNewRouteTemplate] = useState('');

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
    try {
      const result = await createConnector({
        name: newName.trim(),
        display_name: newDisplayName.trim(),
        type: 'inbound_webhook',
      }) as SaturnConnectorCreateResponse;

      if (result?.connector) {
        setSelectedConnector(result.connector);
        setConnectorSecret(result.secret);
        setViewMode('edit');
        showNotification({ message: lang('ConnectorCreated') });
        closeCreate();
        setNewName('');
        setNewDisplayName('');
        loadConnectors();
      }
    } catch (e) {
      showNotification({ message: String(e) });
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

  const handleViewRoutes = useLastCallback(async (connector: SaturnIntegrationConnector) => {
    setSelectedConnector(connector);
    setViewMode('routes');
    try {
      const [routesResult, deliveriesResult] = await Promise.all([
        fetchRoutes(connector.id),
        fetchDeliveries(connector.id),
      ]);
      setRoutes(routesResult?.data || []);
      setDeliveries(deliveriesResult?.data || []);
    } catch {
      // Non-critical
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
    return (
      <div className="settings-content custom-scroll">
        <div className="settings-item">
          <h4>{selectedConnector.display_name}</h4>
          <p className="settings-item-description">{lang('ConnectorName')}: {selectedConnector.name}</p>
          <p className="settings-item-description">{lang('ConnectorType')}: {selectedConnector.type}</p>
          {connectorSecret && (
            <div className="settings-item">
              <p className="settings-item-description">{lang('ConnectorSecret')}</p>
              <code style="word-break: break-all; font-size: 0.75rem">{connectorSecret}</code>
              <Button size="smaller" onClick={() => handleCopySecret(connectorSecret)}>
                Copy
              </Button>
            </div>
          )}
          <div className="settings-item-footer">
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
      </div>
    );
  }

  // Routes view
  if (viewMode === 'routes' && selectedConnector) {
    return (
      <div className="settings-content custom-scroll">
        <div className="settings-item">
          <h4>{selectedConnector.display_name} — {lang('Routes')}</h4>
          <Button onClick={openRouteCreate} color="primary" size="smaller">
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
              contextActions={delivery.status === 'failed' ? [{
                title: lang('RetryDelivery'),
                icon: 'replace',
                handler: () => handleRetryDelivery(delivery.id),
              }] : undefined}
            >
              <span className="title">{delivery.event_type || 'event'}</span>
              <span className="subtitle">{delivery.status} — {new Date(delivery.created_at).toLocaleString()}</span>
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
            <InputText
              label={lang('EventFilter')}
              value={newRouteEventFilter}
              onChange={(e) => setNewRouteEventFilter((e.target as HTMLInputElement).value)}
            />
            <InputText
              label={lang('MessageTemplate')}
              value={newRouteTemplate}
              onChange={(e) => setNewRouteTemplate((e.target as HTMLInputElement).value)}
            />
            <Button onClick={handleCreateRoute} color="primary">
              {lang('CreateRoute')}
            </Button>
          </div>
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
