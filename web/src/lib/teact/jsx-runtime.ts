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
import Teact, { type Props } from './teact';
export type { JSX } from 'react';
export const Fragment = Teact.Fragment;

function create(type: any, props: Props = {}, key?: any) {
  if (key !== undefined) props.key = key;
  const children = props.children;
  if (props.children !== undefined) props.children = undefined;
  return Teact.createElement(type, props, children);
}

export function jsx(type: any, props: Props, key?: any) {
  return create(type, props, key);
}

// Not implemented, reusing jsx for now
export const jsxs = jsx;
export const jsxDEV = jsx;
