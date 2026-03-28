import './ui/initial';
import './ui/settings';
import './api/initial';
import './api/settings';
import './api/sync';
import './api/saturnAuth';
import './apiUpdaters/initial';
// Load critical apiUpdaters early (before BundleMain lazy-loads Main.tsx)
// so that API responses during initial fetch are handled properly
import './apiUpdaters/chats';
import './apiUpdaters/messages';
import './api/chats';
