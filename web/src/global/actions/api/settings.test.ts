const mockHandlers: Record<string, (...args: any[]) => any> = {};

const mockCallApi = jest.fn();
const mockGetGlobal = jest.fn();
const mockSetGlobal = jest.fn();
const mockReplaceSettings = jest.fn((global, settings) => ({
  ...global,
  settings: {
    ...global.settings,
    ...settings,
  },
}));
const mockLoadSaturnSettings = jest.fn();

jest.mock('../../index', () => ({
  __esModule: true,
  addActionHandler: jest.fn((name: string, handler: (...args: any[]) => any) => {
    mockHandlers[name] = handler;
  }),
  getActions: () => ({
    loadSaturnSettings: mockLoadSaturnSettings,
  }),
  getGlobal: (...args: any[]) => mockGetGlobal(...args),
  getPromiseActions: jest.fn(),
  setGlobal: (...args: any[]) => mockSetGlobal(...args),
}));

jest.mock('../../../api/saturn', () => ({
  __esModule: true,
  callApi: (...args: any[]) => mockCallApi(...args),
}));

jest.mock('../../../util/notifications', () => ({
  __esModule: true,
  requestPermission: jest.fn(),
  subscribe: jest.fn(),
  unsubscribe: jest.fn(),
}));

jest.mock('../../../util/oldLangProvider', () => ({
  __esModule: true,
  setTimeFormat: jest.fn(),
}));

jest.mock('../../../util/requestActionTimeout', () => ({
  __esModule: true,
  default: jest.fn(),
}));

jest.mock('../../../util/serverTime', () => ({
  __esModule: true,
  getServerTime: jest.fn(),
}));

jest.mock('../../../util/folderManager', () => ({
  __esModule: true,
  init: jest.fn(),
}));

jest.mock('../../reducers', () => ({
  __esModule: true,
  replaceSettings: (...args: any[]) => mockReplaceSettings(...args),
}));

describe('live translate settings wiring', () => {
  beforeAll(async () => {
    await import('./settings');
    await import('./sync');
  });

  beforeEach(() => {
    jest.clearAllMocks();
    mockGetGlobal.mockReturnValue({
      settings: {
        canTranslate: false,
        canTranslateChats: true,
        translationLanguage: undefined,
      },
    });
  });

  it('maps all Saturn translation settings on the happy path', async () => {
    mockCallApi.mockResolvedValue({
      can_translate: true,
      can_translate_chats: false,
      default_translate_lang: 'es',
    });

    await mockHandlers.loadSaturnSettings({}, {});

    expect(mockCallApi).toHaveBeenCalledWith('getUserSettings');
    expect(mockReplaceSettings).toHaveBeenCalledWith(mockGetGlobal.mock.results[0].value, {
      canTranslate: true,
      canTranslateChats: false,
      translationLanguage: 'es',
    });
    expect(mockSetGlobal).toHaveBeenCalledWith({
      settings: {
        canTranslate: true,
        canTranslateChats: false,
        translationLanguage: 'es',
      },
    });
  });

  it('returns early when Saturn user settings are undefined', async () => {
    mockCallApi.mockResolvedValue(undefined);

    await mockHandlers.loadSaturnSettings({}, {});

    expect(mockCallApi).toHaveBeenCalledWith('getUserSettings');
    expect(mockGetGlobal).not.toHaveBeenCalled();
    expect(mockReplaceSettings).not.toHaveBeenCalled();
    expect(mockSetGlobal).not.toHaveBeenCalled();
  });

  it('falls back canTranslate to false when missing', async () => {
    mockCallApi.mockResolvedValue({
      can_translate_chats: true,
      default_translate_lang: 'fr',
    });

    await mockHandlers.loadSaturnSettings({}, {});

    expect(mockReplaceSettings).toHaveBeenCalledWith(mockGetGlobal.mock.results[0].value, {
      canTranslate: false,
      canTranslateChats: true,
      translationLanguage: 'fr',
    });
  });

  it('falls back canTranslateChats to true when missing', async () => {
    mockCallApi.mockResolvedValue({
      can_translate: true,
      default_translate_lang: 'de',
    });

    await mockHandlers.loadSaturnSettings({}, {});

    expect(mockReplaceSettings).toHaveBeenCalledWith(mockGetGlobal.mock.results[0].value, {
      canTranslate: true,
      canTranslateChats: true,
      translationLanguage: 'de',
    });
  });

  it('calls loadSaturnSettings after sync marks the app as synced', async () => {
    const events: string[] = [];

    mockCallApi.mockImplementation(async (method: string) => {
      if (method === 'fetchChats') {
        return undefined;
      }

      return undefined;
    });

    mockSetGlobal.mockImplementation((global) => {
      if (global?.isSynced) {
        events.push('isSynced');
      }

      if (global?.isSyncing) {
        events.push('isSyncing');
      }
    });
    mockLoadSaturnSettings.mockImplementation(() => {
      events.push('loadSaturnSettings');
    });

    mockHandlers.sync({}, {});

    await Promise.resolve();
    await Promise.resolve();

    expect(events).toEqual(['isSyncing', 'isSynced', 'loadSaturnSettings']);
    expect(mockLoadSaturnSettings).toHaveBeenCalledTimes(1);
  });
});
