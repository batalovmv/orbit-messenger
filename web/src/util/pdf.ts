import { getDocument } from 'pdfjs-dist/legacy/webpack.mjs';

type PdfRenderDimensions = {
  width: number;
  height: number;
};

type PdfPreviewResult = {
  pageCount: number;
};

type PdfPreviewBlobResult = PdfPreviewResult & {
  previewBlobUrl?: string;
};

export const PDF_PREVIEW_ASPECT_RATIO = 0.78;
export const PDF_PREVIEW_WIDTH_REM = 8.25;
export const PDF_PREVIEW_SMALL_WIDTH_REM = 6.5;
export const PDF_PREVIEW_IMAGE_WIDTH = 360;
export const PDF_PREVIEW_IMAGE_HEIGHT = Math.round(PDF_PREVIEW_IMAGE_WIDTH / PDF_PREVIEW_ASPECT_RATIO);

export async function renderPdfPreviewToCanvas(
  url: string,
  canvas: HTMLCanvasElement,
  dimensions: PdfRenderDimensions,
): Promise<PdfPreviewResult | undefined> {
  const context = canvas.getContext('2d', { alpha: false });
  if (!context) {
    return undefined;
  }

  const loadingTask = getDocument({
    url,
    disableAutoFetch: true,
    disableStream: true,
    isEvalSupported: false,
  });
  let shouldDestroyTask = true;

  try {
    const pdf = await loadingTask.promise;
    const firstPage = await pdf.getPage(1);
    const initialViewport = firstPage.getViewport({ scale: 1 });
    const scale = Math.min(
      dimensions.width / initialViewport.width,
      dimensions.height / initialViewport.height,
    );
    const viewport = firstPage.getViewport({ scale });
    const offsetX = Math.max(0, (dimensions.width - viewport.width) / 2);
    const offsetY = Math.max(0, (dimensions.height - viewport.height) / 2);

    canvas.width = dimensions.width;
    canvas.height = dimensions.height;

    context.fillStyle = '#FFFFFF';
    context.fillRect(0, 0, dimensions.width, dimensions.height);

    await firstPage.render({
      canvasContext: context,
      viewport,
      transform: [1, 0, 0, 1, offsetX, offsetY],
    }).promise;

    firstPage.cleanup();
    await pdf.cleanup();
    shouldDestroyTask = false;

    return {
      pageCount: pdf.numPages,
    };
  } catch {
    return undefined;
  } finally {
    if (shouldDestroyTask) {
      await loadingTask.destroy().catch(() => undefined);
    }
  }
}

export async function createPdfPreviewBlobUrl(
  url: string,
  dimensions: PdfRenderDimensions,
): Promise<PdfPreviewBlobResult | undefined> {
  const canvas = document.createElement('canvas');
  const renderResult = await renderPdfPreviewToCanvas(url, canvas, dimensions);

  if (!renderResult) {
    return undefined;
  }

  const blob = await new Promise<Blob | undefined>((resolve) => {
    canvas.toBlob((nextBlob) => {
      resolve(nextBlob || undefined);
    }, 'image/png');
  });

  return {
    ...renderResult,
    previewBlobUrl: blob ? URL.createObjectURL(blob) : undefined,
  };
}
