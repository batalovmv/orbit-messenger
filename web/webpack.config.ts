import 'webpack-dev-server';
import 'dotenv/config';

import WatchFilePlugin from '@mytonwallet/webpack-watch-file-plugin';
import StatoscopeWebpackPlugin from '@statoscope/webpack-plugin';
import CopyWebpackPlugin from 'copy-webpack-plugin';
import { statSync } from 'fs';
import { GitRevisionPlugin } from 'git-revision-webpack-plugin';
import HtmlWebpackPlugin from 'html-webpack-plugin';
import MiniCssExtractPlugin from 'mini-css-extract-plugin';
import path from 'path';
import type { Compiler, Configuration } from 'webpack';
import {
  ContextReplacementPlugin,
  DefinePlugin,
  EnvironmentPlugin,
  NormalModuleReplacementPlugin,
  ProvidePlugin,
} from 'webpack';

import { PRODUCTION_URL } from './src/config.ts';
import { version as appVersion } from './package.json' with { type: 'json' };

const {
  HEAD,
  APP_ENV = 'production',
  APP_MOCKED_CLIENT = '',
  HTTPS_CERT_PATH = '',
  HTTPS_KEY_PATH = '',
} = process.env;

const DEFAULT_APP_TITLE = `Orbit Messenger${APP_ENV !== 'production' ? ' Beta' : ''}`;

if (APP_ENV === 'production' && !process.env.VAPID_PUBLIC_KEY) {
  throw new Error('VAPID_PUBLIC_KEY is required for production web push.');
}

// GitHub workflow uses an empty string as the default value if it's not in repository variables, so we cannot define a default value here
process.env.BASE_URL = process.env.BASE_URL || PRODUCTION_URL;

const {
  BASE_URL,
  APP_TITLE = DEFAULT_APP_TITLE,
} = process.env;

const CSP = `
  default-src 'self';
  connect-src 'self' https://*.saturn.ac wss://*.saturn.ac blob:
    ${APP_ENV === 'development' ? 'http://localhost:* ws://localhost:* ipc:' : ''};
  script-src 'self' 'wasm-unsafe-eval'${APP_ENV === 'development' ? ' \'unsafe-inline\'' : ''};
  style-src 'self' 'unsafe-inline';
  img-src 'self' data: blob:;
  media-src 'self' blob: data:;
  object-src 'none';
  frame-src 'self' blob:;
  worker-src 'self' blob:;
  base-uri 'none';
  form-action 'none';`
  .replace(/\s+/g, ' ').trim();

const CHANGELOG_PATH = path.resolve(__dirname, 'src/versionNotification.txt');

export default function createConfig(
  _: any,
  { mode = 'production' }: { mode: 'none' | 'development' | 'production' },
): Configuration {
  let server: Required<Configuration>['devServer']['server'] = 'http';
  if (HTTPS_CERT_PATH && HTTPS_KEY_PATH) {
    server = {
      type: 'https',
      options: {
        key: HTTPS_KEY_PATH,
        cert: HTTPS_CERT_PATH,
      },
    };
  }

  return {
    mode,
    cache: mode === 'development' ? false : undefined,
    // The SW is its own entry so it gets a stable output filename
    // (`serviceWorker.js`, no contenthash). The browser identifies a
    // SW registration by its script URL — if that URL changes on every
    // deploy, each register() creates a NEW registration alongside the
    // old one instead of running the proper "update found → installing
    // → activate" flow that skipWaiting + clients.claim is built around.
    // Stable URL → byte-by-byte content compare → real update flow.
    entry: {
      main: './src/index.tsx',
      serviceWorker: {
        import: './src/serviceWorker/index.ts',
        filename: 'serviceWorker.js',
      },
    },
    target: 'web',

    devServer: {
      // Honour PORT env var so `preview_start` (which allocates a free
      // port via autoPort) can point webpack at the port it picked.
      // Falls back to 3000 for plain `npm run dev`.
      port: Number(process.env.PORT) || 3000,
      host: '0.0.0.0',
      allowedHosts: 'all',
      hot: false,
      client: {
        overlay: false,
      },
      server,
      static: [
        {
          directory: path.resolve(__dirname, 'public'),
        },
        {
          directory: path.resolve(__dirname, 'node_modules/emoji-data-ios'),
        },
        {
          directory: path.resolve(__dirname, 'node_modules/opus-recorder/dist'),
        },
        {
          directory: path.resolve(__dirname, 'src/lib/rlottie'),
        },
        {
          directory: path.resolve(__dirname, 'src/lib/video-preview'),
        },
        {
          directory: path.resolve(__dirname, 'src/lib/secret-sauce'),
        },
      ],
      devMiddleware: {
        stats: 'minimal',
      },
      proxy: [
        {
          context: ['/api'],
          target: 'http://localhost:8080',
          changeOrigin: true,
          ws: true,
        },
      ],
      headers: {
        // CSP is set via <meta> tag in index.html — no HTTP header needed.
        // Duplicate headers cause the browser to enforce the most restrictive union.
      },
    },

    output: {
      filename: '[name].[contenthash].js',
      chunkFilename: '[id].[chunkhash].js',
      assetModuleFilename: '[name].[contenthash][ext]',
      path: path.resolve(__dirname, 'dist'),
      clean: true,
    },

    module: {
      rules: [
        {
          test: /rlottie-wasm\.js$/,
          type: 'asset/resource',
        },
        {
          test: /\.(ts|tsx|js|mjs|cjs)$/,
          loader: 'babel-loader',
          exclude: [/node_modules/, /rlottie-wasm\.js$/],
        },
        {
          test: /\.css$/,
          use: [
            MiniCssExtractPlugin.loader,
            {
              loader: 'css-loader',
              options: {
                importLoaders: 1,
                modules: {
                  namedExport: false,
                  auto: true,
                },
              },
            },
            'postcss-loader',
          ],
        },
        {
          test: /\.scss$/,
          use: [
            MiniCssExtractPlugin.loader,
            {
              loader: 'css-loader',
              options: {
                modules: {
                  namedExport: false,
                  exportLocalsConvention: 'camelCase',
                  auto: true,
                  localIdentName: APP_ENV === 'production' ? '[sha1:hash:base64:8]' : '[name]__[local]',
                },
              },
            },
            'postcss-loader',
            'sass-loader',
          ],
        },
        {
          test: /\.(woff(2)?|ttf|eot|svg|png|jpg|tgs|webp)(\?v=\d+\.\d+\.\d+)?$/,
          type: 'asset/resource',
        },
        {
          test: /\.wasm$/,
          type: 'asset/resource',
        },
        {
          test: /\.(txt|tl|strings)$/i,
          type: 'asset/source',
        },
      ],
    },

    resolve: {
      extensions: ['.js', '.cjs', '.mjs', '.ts', '.tsx'],
      alias: {
        '@teact$': path.resolve(__dirname, './src/lib/teact/teact.ts'),
        '@teact': path.resolve(__dirname, './src/lib/teact'),
      },
      fallback: {
        path: require.resolve('path-browserify'),
        os: require.resolve('os-browserify/browser'),
        buffer: require.resolve('buffer/'),
        fs: false,
        crypto: false,
      },
    },

    plugins: [
      // Clearing of the unused files for code highlight for smaller chunk count
      new ContextReplacementPlugin(
        /highlight\.js[\\/]lib[\\/]languages/,
        /^((?!\.js\.js).)*$/,
      ),
      ...(APP_MOCKED_CLIENT === '1'
        ? [new NormalModuleReplacementPlugin(
          /src[\\/]lib[\\/]gramjs[\\/]client[\\/]TelegramClient\.js/,
          './MockClient.ts',
        )]
        : []),
      new HtmlWebpackPlugin({
        appTitle: APP_TITLE,
        appleIcon: APP_ENV === 'production' ? 'apple-touch-icon' : 'apple-touch-icon-dev',
        mainIcon: APP_ENV === 'production' ? 'icon-192x192' : 'icon-dev-192x192',
        manifest: APP_ENV === 'production' ? 'site.webmanifest' : 'site_dev.webmanifest',
        baseUrl: BASE_URL,
        csp: CSP,
        template: 'src/index.html',
        // The SW entry must NOT be injected into index.html as a <script>.
        // It's loaded by the browser via navigator.serviceWorker.register()
        // in setupServiceWorker.ts; injecting it here would execute SW
        // globals (self.addEventListener('push', ...)) on the window.
        excludeChunks: ['serviceWorker'],
      }),
      new CopyWebpackPlugin({
        patterns: [
          { from: path.resolve(__dirname, 'public'), to: '.' },
          { from: path.resolve(__dirname, 'node_modules/emoji-data-ios/img-apple-64'), to: 'img-apple-64' },
          { from: path.resolve(__dirname, 'node_modules/emoji-data-ios/img-apple-160'), to: 'img-apple-160' },
        ],
      }),
      new MiniCssExtractPlugin({
        filename: '[name].[contenthash].css',
        chunkFilename: '[name].[chunkhash].css',
        ignoreOrder: true,
      }),
      new EnvironmentPlugin({
        APP_ENV,
        APP_MOCKED_CLIENT,
        // eslint-disable-next-line no-null/no-null
        APP_NAME: null,
        APP_TITLE,
        VAPID_PUBLIC_KEY: process.env.VAPID_PUBLIC_KEY || '',
        TELEGRAM_API_ID: '0', // Not used by Saturn API layer
        TELEGRAM_API_HASH: '0', // Not used by Saturn API layer
        // eslint-disable-next-line no-null/no-null
        TEST_SESSION: null,
        BASE_URL,
        SATURN_API_URL: process.env.SATURN_API_URL || 'http://localhost:8080/api/v1',
        SATURN_GATEWAY_HOST: process.env.SATURN_GATEWAY_HOST || '',
      }),
      // Updates each dev re-build to provide current git branch or commit hash
      new DefinePlugin({
        APP_VERSION: JSON.stringify(appVersion),
        APP_REVISION: DefinePlugin.runtimeValue(() => {
          const { branch, commit } = getGitMetadata();
          const shouldDisplayOnlyCommit = APP_ENV === 'staging' || !branch || branch === 'HEAD';
          return JSON.stringify(shouldDisplayOnlyCommit ? commit : `${branch}#${commit}`);
        }, mode === 'development' ? true : []),
        CHANGELOG_DATETIME: DefinePlugin.runtimeValue(() => {
          return JSON.stringify(statSync(CHANGELOG_PATH, { throwIfNoEntry: false })?.mtime.getTime());
        }, {
          fileDependencies: [CHANGELOG_PATH],
        }),
      }),
      new ProvidePlugin({
        Buffer: ['buffer', 'Buffer'],
      }),
      new StatoscopeWebpackPlugin({
        statsOptions: {
          context: __dirname,
        },
        saveReportTo: path.resolve('./public/statoscope-report.html'),
        saveStatsTo: path.resolve('./public/build-stats.json'),
        normalizeStats: true,
        open: false,
        extensions: [new WebpackContextExtension()],
      }),
      // WebpackObfuscator disabled — causes rlottie-wasm worker hash mismatches
      // and is unnecessary for a corporate messenger
      ...(mode === 'development' ? [
        new WatchFilePlugin({
          rules: [
            {
              files: 'src/assets/localization/fallback.strings',
              action: 'npm run lang:ts',
            },
            {
              files: 'src/lib/gramjs/tl/static/**/*',
              action: 'npm run gramjs:tl',
              sharedAction: true,
            },
            {
              files: 'src/assets/font-icons/*.svg',
              action: 'npm run icons:build',
              sharedAction: true,
            },
          ],
        }),
      ] : []),
    ],

    devtool: mode === 'production' ? false : 'source-map',

    optimization: {
      splitChunks: {
        cacheGroups: {
          sharedComponents: {
            name: 'shared-components',
            test: /[\\/]src[\\/]components[\\/]ui[\\/]/,
          },
        },
      },
      ...(APP_ENV === 'staging' && {
        chunkIds: 'named',
      }),
    },
  };
}

function getGitMetadata() {
  const gitRevisionPlugin = new GitRevisionPlugin();
  const branch = HEAD || gitRevisionPlugin.branch();
  const commit = gitRevisionPlugin.commithash()?.substring(0, 7);
  return { branch, commit };
}

class WebpackContextExtension {
  context: string;

  constructor() {
    this.context = '';
  }

  handleCompiler(compiler: Compiler) {
    this.context = compiler.context;
  }

  getExtension() {
    return {
      descriptor: { name: 'custom-webpack-extension-context', version: '1.0.0' },
      payload: { context: this.context },
    };
  }
}
