import { addCallback } from '../lib/teact/teactn';

import type { GlobalState } from './types';

import { selectTabState } from './selectors';

let intervals: number[] = [];

let prevGlobal: GlobalState | undefined;

addCallback((global: GlobalState) => {
  const previousGlobal = prevGlobal;
  prevGlobal = global;

  const isCurrentMaster = selectTabState(global)?.isMasterTab;
  const isPreviousMaster = previousGlobal && selectTabState(previousGlobal)?.isMasterTab;
  if (isCurrentMaster === isPreviousMaster) return;

  if (isCurrentMaster && !isPreviousMaster) {
    startIntervals();
  } else {
    stopIntervals();
  }
});

function startIntervals() {
  if (intervals.length) return;
  // No recurring intervals needed currently
}

function stopIntervals() {
  intervals.forEach((interval) => clearInterval(interval));
  intervals = [];
}
