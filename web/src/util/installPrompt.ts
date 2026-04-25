import { getActions } from '../global';

let promptInstall: (() => Promise<void>) | undefined;

function isAppInstalled(): boolean {
  if (window.matchMedia?.('(display-mode: standalone)').matches) {
    return true;
  }
  if ((window.navigator as { standalone?: boolean }).standalone === true) {
    return true;
  }
  return false;
}

export function setupBeforeInstallPrompt() {
  if (isAppInstalled()) {
    getActions().setInstallPrompt({ canInstall: false });
    return;
  }

  window.addEventListener('beforeinstallprompt', (e: any) => {
    e.preventDefault();

    promptInstall = async () => {
      e.prompt();
      const userChoice = await e.userChoice;
      const isInstalled = userChoice.outcome === 'accepted';

      if (!isInstalled) return;
      promptInstall = undefined;
      getActions().setInstallPrompt({ canInstall: false });
    };
    getActions().setInstallPrompt({ canInstall: true });
  });

  window.addEventListener('appinstalled', () => {
    promptInstall = undefined;
    getActions().setInstallPrompt({ canInstall: false });
  });
}

export function getPromptInstall() {
  return promptInstall;
}
