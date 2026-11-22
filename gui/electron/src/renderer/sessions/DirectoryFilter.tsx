import { useState } from "react";
import { Icon } from "../components/Icon";

type Status = "all" | "online" | "offline";
export function useDirectoryFilter() {
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<Status>("all");
  const needle = query.trim().toLowerCase();
  return {
    query,
    setQuery,
    status,
    setStatus,
    active: !!needle || status !== "all",
    matchesText: (...values: string[]) =>
      values.some((value) => value.toLowerCase().includes(needle)),
    matchesStatus: (online: boolean) =>
      status === "all" || (status === "online") === online,
  };
}

type Translate = (id: string) => string;
export function DirectoryFilter({
  filter,
  t,
}: {
  filter: ReturnType<typeof useDirectoryFilter>;
  t: Translate;
}) {
  return (
    <div className="directory-filters">
      <label className="session-search">
        <Icon name="search" />
        <input
          type="search"
          value={filter.query}
          onChange={(e) => filter.setQuery(e.target.value)}
          aria-label={t("workbench.search")}
          placeholder={t("workbench.search")}
        />
      </label>
      <select
        aria-label={t("directory.filter")}
        title={t("directory.filter")}
        value={filter.status}
        onChange={(e) => filter.setStatus(e.target.value as Status)}
      >
        <option value="all">{t("directory.all")}</option>
        <option value="online">{t("directory.online")}</option>
        <option value="offline">{t("directory.offline")}</option>
      </select>
    </div>
  );
}

export function ConnectionLight({
  online,
  t,
}: {
  online: boolean;
  t: Translate;
}) {
  const label = t(online ? "directory.online" : "directory.offline");
  return (
    <span
      role="img"
      aria-label={label}
      title={label}
      className={"dot directory-light " + (online ? "open" : "offline")}
    />
  );
}
