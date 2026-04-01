import type { ApiTypeStory } from '../api/types';

// Stories feature removed — this hook is a no-op
function useEnsureStory(
  _peerId?: string,
  _storyId?: number,
  _story?: ApiTypeStory,
) {
  // no-op
}

export default useEnsureStory;
