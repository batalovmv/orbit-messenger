/*
 * Orbit Messenger
 * Copyright (C) 2026 MST Corp.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */
import { onIdle, throttleWith } from '../../util/schedulers';
import { createSignal } from '../../util/signals';
import { requestMeasure } from '../fasterdom/fasterdom';

const AUTO_END_TIMEOUT = 1000;

let counter = 0;
let counterBlocking = 0;

const [getIsAnimating, setIsAnimating] = createSignal(false);
const [getIsBlockingAnimating, setIsBlockingAnimating] = createSignal(false);

export const getIsHeavyAnimating = getIsAnimating;
export { getIsBlockingAnimating };

export function beginHeavyAnimation(duration = AUTO_END_TIMEOUT, isBlocking = false) {
  counter++;

  if (counter === 1) {
    setIsAnimating(true);
  }

  if (isBlocking) {
    counterBlocking++;

    if (counterBlocking === 1) {
      setIsBlockingAnimating(true);
    }
  }

  const timeout = window.setTimeout(onEnd, duration);

  let hasEnded = false;

  function onEnd() {
    if (hasEnded) return;
    hasEnded = true;

    clearTimeout(timeout);

    counter--;

    if (counter === 0) {
      setIsAnimating(false);
    }

    if (isBlocking) {
      counterBlocking--;

      if (counterBlocking === 0) {
        setIsBlockingAnimating(false);
      }
    }
  }

  return onEnd;
}

export function onFullyIdle(cb: NoneToVoidFunction) {
  onIdle(() => {
    if (getIsAnimating()) {
      requestMeasure(() => {
        onFullyIdle(cb);
      });
    } else {
      cb();
    }
  });
}

export function throttleWithFullyIdle<F extends AnyToVoidFunction>(fn: F) {
  return throttleWith(onFullyIdle, fn);
}
