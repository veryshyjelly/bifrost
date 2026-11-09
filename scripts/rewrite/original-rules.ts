import { pathToFileURL } from 'node:url';
import { oracle } from './oracle.ts';

globalThis.fetch = async () => { throw new Error('Network is forbidden in rule replay'); };
const baseline = oracle();
const format = await import(pathToFileURL(`${baseline}/src/format.ts`).href);
const config = await import(pathToFileURL(`${baseline}/src/config.ts`).href);
const { cronSchedule } = await import(pathToFileURL(`${baseline}/src/cron.ts`).href);
const input = await Bun.stdin.json();
const clock = Date.now;
const result = input.map((row: any) => {
  Date.now = () => row.now;
  try {
    let value;
    if (row.op === 'cron') value = row.args[2].map((date: string) => cronSchedule(row.args[0], row.args[1])(new Date(date)));
    else if (row.op === 'validate') { config.validate(config.configuration(row.args[0])); value = true; }
    else value = (format[row.op] || config[row.op])(...row.args);
    return { value };
  } catch (error) { return { error: (error as Error).message }; }
  finally { Date.now = clock; }
});
console.log(JSON.stringify(result));
