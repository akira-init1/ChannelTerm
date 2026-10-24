/** Canonical client encodings; independent of connection transport and Host metadata. */
export const terminalEncodings = [
  {
    name: "utf-8",
    aliases: ["utf8"],
  },
  {
    name: "ascii",
    aliases: ["us-ascii"],
  },
  {
    name: "gbk",
    aliases: ["cp936", "windows-936"],
  },
  {
    name: "gb18030",
    aliases: [],
  },
  {
    name: "big5",
    aliases: ["cp950"],
  },
  {
    name: "shift_jis",
    aliases: ["shift-jis", "sjis", "cp932"],
  },
  {
    name: "euc-jp",
    aliases: ["eucjp"],
  },
  {
    name: "euc-kr",
    aliases: ["euckr", "cp949"],
  },
  {
    name: "windows-874",
    aliases: ["cp874"],
  },
  {
    name: "windows-1250",
    aliases: ["cp1250"],
  },
  {
    name: "windows-1251",
    aliases: ["cp1251"],
  },
  {
    name: "windows-1252",
    aliases: ["cp1252"],
  },
  {
    name: "windows-1253",
    aliases: ["cp1253"],
  },
  {
    name: "windows-1254",
    aliases: ["cp1254"],
  },
  {
    name: "windows-1255",
    aliases: ["cp1255"],
  },
  {
    name: "windows-1256",
    aliases: ["cp1256"],
  },
  {
    name: "windows-1257",
    aliases: ["cp1257"],
  },
  {
    name: "windows-1258",
    aliases: ["cp1258"],
  },
  {
    name: "iso-8859-1",
    aliases: ["latin1"],
  },
  {
    name: "iso-8859-2",
    aliases: ["latin2"],
  },
  {
    name: "iso-8859-3",
    aliases: [],
  },
  {
    name: "iso-8859-4",
    aliases: [],
  },
  {
    name: "iso-8859-5",
    aliases: [],
  },
  {
    name: "iso-8859-6",
    aliases: [],
  },
  {
    name: "iso-8859-7",
    aliases: [],
  },
  {
    name: "iso-8859-8",
    aliases: [],
  },
  {
    name: "iso-8859-9",
    aliases: [],
  },
  {
    name: "iso-8859-10",
    aliases: [],
  },
  {
    name: "iso-8859-13",
    aliases: [],
  },
  {
    name: "iso-8859-14",
    aliases: [],
  },
  {
    name: "iso-8859-15",
    aliases: [],
  },
  {
    name: "iso-8859-16",
    aliases: [],
  },
  {
    name: "koi8-r",
    aliases: ["koi8r"],
  },
  {
    name: "koi8-u",
    aliases: ["koi8u"],
  },
  {
    name: "cp437",
    aliases: ["ibm437"],
  },
  {
    name: "cp850",
    aliases: ["ibm850"],
  },
  {
    name: "cp866",
    aliases: ["ibm866"],
  },
] as const;
export type TerminalEncoding = (typeof terminalEncodings)[number]["name"];
export function normalizeTerminalEncoding(
  value: string,
): TerminalEncoding | undefined {
  const name = value.trim().toLowerCase();
  return terminalEncodings.find(
    (entry) =>
      entry.name === name || entry.aliases.some((alias) => alias === name),
  )?.name;
}
