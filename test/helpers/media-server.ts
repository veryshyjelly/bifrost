import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

export async function mediaServer(t: any, serve: (req: IncomingMessage, res: ServerResponse) => void) {
  const directory = await mkdtemp(join(tmpdir(), 'bifrost-history-media-'));
  const server = createServer(serve);
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  t.after(async () => {
    server.closeAllConnections();
    await new Promise<void>(resolve => server.close(() => resolve()));
    await rm(directory, { recursive: true, force: true });
  });
  return { directory, url: `http://127.0.0.1:${(server.address() as any).port}` };
}

export function mediaError(res: ServerResponse, status: number, code: string) {
  res.writeHead(status, { 'content-type': 'application/json', 'retry-after': '2' });
  res.end(JSON.stringify({ statusCode: status, code }));
}
