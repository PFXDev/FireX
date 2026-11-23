import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowDownIcon,
  ArrowLeftIcon,
  ArrowRightIcon,
  ArrowUpIcon,
  ChevronDownIcon,
  EyeIcon,
  FileTextIcon,
  GripVerticalIcon,
  LayoutTemplateIcon,
  ListTreeIcon,
  PencilIcon,
  PlusIcon,
  RouteIcon,
  SaveIcon,
  Trash2Icon,
  TriangleAlertIcon,
  XIcon,
} from "lucide-react";
import { toast } from "sonner";

import { api } from "@/api";
import type {
  Egress,
  EgressMember,
  MemberKind,
  NodeGroup,
  Policy,
  Profile,
  RoutingMatrix,
  RoutingPreset,
  RuleSet,
} from "@/api";
import { CodeBlock, CodeTextarea } from "@/components/code-display";
import { ConfirmDialog } from "@/components/confirm-dialog";
import {
  LoadErrorAlert,
  RefreshButton,
  TableSkeleton,
} from "@/components/load-state";
import { NumberInput } from "@/components/number-input";
import { PageHeader } from "@/components/page-header";
import { StatusBadge } from "@/components/status-badge";
import { StickySaveBar } from "@/components/sticky-save-bar";
import { SearchInput } from "@/components/table-toolbar";
import {
  Alert,
  AlertAction,
  AlertDescription,
  AlertTitle,
} from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useDirtyDialog, useUnsavedGuard } from "@/hooks/use-unsaved-guard";
import { errorMessage } from "@/lib/format";
import { useSession } from "@/lib/session";
import { reportSyncError } from "@/lib/sync";
import {
  dropRuleSetReferences,
  renameRuleSetReferences,
  routingDocument,
  RULE_SET_TYPE,
  ruleSetLineCount,
  ruleSetUsers,
  type RoutingRow as Row,
} from "@/lib/routing-matrix";
import { cn } from "@/lib/utils";

/** profileId 0 addresses the default column every profile falls back to. */
const DEFAULT_COLUMN = 0;

let rowKeySeq = 0;
function createRowKey(policyId?: number): string {
  return policyId ? `policy-${policyId}` : `row-${++rowKeySeq}`;
}

function clonePolicy(policy: Policy): Policy {
  return {
    ...policy,
    rules: policy.rules.map((rule) => ({ ...rule })),
  };
}

function cloneRuleSet(set: RuleSet): RuleSet {
  return { ...set };
}

function cloneEgress(egress: Egress): Egress {
  return {
    ...egress,
    members: egress.members.map((member) => ({ ...member })),
  };
}

function reorder<T>(list: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) {
    return list;
  }
  const next = [...list];
  const [item] = next.splice(from, 1);
  next.splice(to, 0, item);
  return next;
}

type PolicyEditorState = {
  index: number | null;
  initial: Policy;
  draft: Policy;
};

type RuleSetEditorState = {
  index: number | null;
  initial: RuleSet;
  draft: RuleSet;
};

type CellEditorState = {
  row: number;
  profileId: number;
  initial: Egress | null;
  draft: Egress | null;
};

type ProfileDraft = {
  id?: number;
  name: string;
  allGroups: boolean;
  enabled: boolean;
  remark: string;
  groupIds: number[];
};

function blankEgress(profileId: number): Egress {
  return {
    policyIndex: 0,
    profileId,
    type: "select",
    testUrl: "",
    interval: 300,
    tolerance: 50,
    hidden: false,
    clientHidden: false,
    members: [],
  };
}

function move<T>(list: T[], index: number, delta: number): T[] {
  const target = index + delta;
  if (target < 0 || target >= list.length) return list;
  const next = [...list];
  const [item] = next.splice(index, 1);
  next.splice(target, 0, item);
  return next;
}

export function RoutingPage() {
  const { readOnly } = useSession();
  const [rows, setRows] = useState<Row[]>([]);
  const [ruleSets, setRuleSets] = useState<RuleSet[]>([]);
  const [options, setOptions] = useState<RoutingMatrix["options"] | null>(null);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [groups, setGroups] = useState<NodeGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [retrying, setRetrying] = useState(false);
  const [refreshingProfiles, setRefreshingProfiles] = useState(false);
  const [saving, setSaving] = useState(false);
  const [profileSaving, setProfileSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [needsReview, setNeedsReview] = useState(false);
  const [matrixScrolled, setMatrixScrolled] = useState(false);

  const [policyEditor, setPolicyEditor] = useState<PolicyEditorState | null>(null);
  const [ruleSetEditor, setRuleSetEditor] = useState<RuleSetEditorState | null>(null);
  const [pendingRuleSetDelete, setPendingRuleSetDelete] = useState<
    number | null
  >(null);
  const [cellEditor, setCellEditor] = useState<CellEditorState | null>(null);
  const [profileDraft, setProfileDraft] = useState<ProfileDraft | null>(null);
  const [initialProfileDraft, setInitialProfileDraft] = useState<ProfileDraft | null>(null);
  const [pendingProfileDelete, setPendingProfileDelete] =
    useState<Profile | null>(null);
  const [pendingPolicyDelete, setPendingPolicyDelete] = useState<number | null>(
    null,
  );
  const [pendingPreset, setPendingPreset] = useState<RoutingPreset | null>(
    null,
  );
  const [confirmDiscard, setConfirmDiscard] = useState(false);
  const [dragRowIndex, setDragRowIndex] = useState<number | null>(null);
  const [dragOverRowIndex, setDragOverRowIndex] = useState<number | null>(null);

  const [previewProfile, setPreviewProfile] = useState<number>(DEFAULT_COLUMN);
  const [preview, setPreview] = useState<{
    yaml?: string;
    error?: string;
  } | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const previewRequest = useRef(0);

  const load = useCallback(async () => {
    try {
      const [matrix, nextProfiles, nextGroups] = await Promise.all([
        api.get<RoutingMatrix>("/routing"),
        api.get<Profile[]>("/profiles"),
        api.get<NodeGroup[]>("/node-groups"),
      ]);
      const byIndex = new Map<number, Map<number, Egress>>();
      matrix.egresses.forEach((egress) => {
        if (!byIndex.has(egress.policyIndex))
          byIndex.set(egress.policyIndex, new Map());
        byIndex.get(egress.policyIndex)!.set(egress.profileId, egress);
      });
      setRows(
        matrix.policies.map((policy, index) => ({
          _key: createRowKey(policy.id),
          policy,
          cells: byIndex.get(index) ?? new Map(),
        })),
      );
      setRuleSets(matrix.ruleSets ?? []);
      setOptions(matrix.options);
      setNeedsReview(matrix.needsReview);
      setProfiles(nextProfiles);
      setGroups(nextGroups);
      setLoadError(null);
      setDirty(false);
      return true;
    } catch (err) {
      setLoadError(errorMessage(err, "分流配置加载失败"));
      return false;
    }
  }, []);

  /**
   * Profiles are saved on their own, outside the matrix draft. Reloading only
   * them keeps whatever the operator has edited in the cells; a full reload
   * here would throw those edits away without a word.
   */
  const reloadProfiles = useCallback(async () => {
    try {
      const [nextProfiles, nextGroups] = await Promise.all([
        api.get<Profile[]>("/profiles"),
        api.get<NodeGroup[]>("/node-groups"),
      ]);
      setProfiles(nextProfiles);
      setGroups(nextGroups);
    } catch (err) {
      toast.error(errorMessage(err, "方案列表刷新失败"));
    }
  }, []);

  // Operators get no write affordances, so they can never hold unsaved edits.
  useUnsavedGuard(dirty && !readOnly);

  useEffect(() => {
    void load().finally(() => setLoading(false));
  }, [load]);

  const patchRows = (next: Row[]) => {
    setRows(next);
    setDirty(true);
  };

  const save = async (): Promise<boolean> => {
    if (saving) return false;
    setSaving(true);
    previewRequest.current++;
    setPreviewing(false);
    setPreview(null);
    try {
      await api.put("/routing", routingDocument(rows, ruleSets));
      setDirty(false);
      toast.success("分流已保存");
      if (!(await load())) {
        toast.warning("分流已保存，但刷新失败，请重新加载");
      }
      return true;
    } catch (err) {
      toast.error(errorMessage(err, "保存失败"));
      return false;
    } finally {
      setSaving(false);
    }
  };

  const runPreview = async () => {
    if (previewing) return;
    const requestId = ++previewRequest.current;
    setPreviewing(true);
    setPreview(null);
    try {
      const result = await api.get<{ yaml?: string; error?: string }>(
        `/routing/preview?profileId=${previewProfile}`,
      );
      if (requestId === previewRequest.current) setPreview(result);
    } catch (err) {
      if (requestId === previewRequest.current) setPreview({ error: errorMessage(err, "预览失败") });
    } finally {
      if (requestId === previewRequest.current) setPreviewing(false);
    }
  };

  const saveAndPreview = async () => {
    if (await save()) await runPreview();
  };

  useEffect(() => {
    previewRequest.current++;
    setPreview(null);
    setPreviewing(false);
    if (previewProfile !== DEFAULT_COLUMN && !profiles.some((profile) => profile.id === previewProfile)) {
      setPreviewProfile(DEFAULT_COLUMN);
    }
  }, [profiles, previewProfile]);

  const groupByName = useMemo(
    () => new Map(groups.map((group) => [group.name, group])),
    [groups],
  );

  const memberLabel = useCallback(
    (member: EgressMember): string => {
      switch (member.kind) {
        case "node-group": {
          const found = groupByName.get(member.ref);
          return found
            ? found.emoji
              ? `${found.emoji} ${found.name}`
              : found.name
            : `${member.ref}（未找到）`;
        }
        case "policy": {
          const found = rows.find((row) => row.policy.name === member.ref);
          return found
            ? found.policy.icon
              ? `${found.policy.icon} ${found.policy.name}`
              : found.policy.name
            : `${member.ref}（未找到）`;
        }
        case "all-node-groups":
          return "全部节点组";
        case "all-inbounds":
          return "全部入站";
        default:
          return member.ref;
      }
    },
    [groupByName, rows],
  );

  /** A member pointing at a node group or policy that no longer exists. */
  const memberMissing = useCallback(
    (member: EgressMember): boolean =>
      (member.kind === "node-group" && !groupByName.has(member.ref)) ||
      (member.kind === "policy" &&
        !rows.some((row) => row.policy.name === member.ref)),
    [groupByName, rows],
  );

  /** The egress that actually applies to a column, default included. */
  const effective = (
    row: Row,
    profileId: number,
  ): { egress: Egress; inherited: boolean } | null => {
    const own = row.cells.get(profileId);
    if (own) return { egress: own, inherited: false };
    const fallback = row.cells.get(DEFAULT_COLUMN);
    if (fallback) return { egress: fallback, inherited: true };
    return null;
  };

  const setCell = (
    rowIndex: number,
    profileId: number,
    egress: Egress | null,
  ) => {
    const next = rows.map((row, i) => {
      if (i !== rowIndex) return row;
      const cells = new Map(row.cells);
      if (egress) cells.set(profileId, { ...egress, profileId });
      else cells.delete(profileId);
      return { ...row, cells };
    });
    patchRows(next);
  };

  const openCellEditor = (row: number, profileId: number) => {
    const own = rows[row].cells.get(profileId);
    const copy = own ? cloneEgress(own) : null;
    setCellEditor({
      row,
      profileId,
      initial: copy ? cloneEgress(copy) : null,
      draft: copy,
    });
  };

  const openPolicyEditor = (index: number) => {
    const target = clonePolicy(rows[index].policy);
    setPolicyEditor({
      index,
      initial: clonePolicy(target),
      draft: target,
    });
  };

  const openAddPolicy = () => {
    let name = "新策略";
    for (let i = 2; rows.some((row) => row.policy.name === name); i++)
      name = `新策略 ${i}`;
    const blank: Policy = {
      id: 0,
      name,
      icon: "",
      isFinal: false,
      enabled: true,
      remark: "",
      rules: [],
    };
    setPolicyEditor({
      index: null,
      initial: clonePolicy(blank),
      draft: blank,
    });
  };

  /**
   * Members reference a policy by bare name; the server rewrites them on save
   * as well, but the draft has to follow too or the other cells would show a
   * dangling reference until then.
   */
  const confirmPolicyEditor = (nextPolicy: Policy) => {
    if (!policyEditor) return;
    const normalized: Policy = {
      ...clonePolicy(nextPolicy),
      name: nextPolicy.name.trim(),
    };
    if (policyEditor.index === null) {
      const cells = new Map<number, Egress>();
      cells.set(DEFAULT_COLUMN, {
        ...blankEgress(DEFAULT_COLUMN),
        members: [{ kind: "all-node-groups", ref: "" }],
      });
      const baseRows = normalized.isFinal
        ? rows.map((row) => ({ ...row, policy: { ...row.policy, isFinal: false } }))
        : rows;
      patchRows([
        ...baseRows,
        {
          _key: createRowKey(),
          policy: normalized,
          cells,
        },
      ]);
    } else {
      const idx = policyEditor.index;
      const previous = rows[idx].policy.name;
      const renamed = previous !== normalized.name;
      patchRows(
        rows.map((row, i) => ({
          ...row,
          policy:
            i === idx
              ? normalized
              : normalized.isFinal
                ? { ...row.policy, isFinal: false }
                : row.policy,
          cells: renamed
            ? new Map(
                [...row.cells.entries()].map(([profileId, egress]) => [
                  profileId,
                  {
                    ...egress,
                    members: egress.members.map((m) =>
                      m.kind === "policy" && m.ref === previous
                        ? { ...m, ref: normalized.name }
                        : m,
                    ),
                  },
                ]),
              )
            : row.cells,
        })),
      );
    }
    setPolicyEditor(null);
  };

  const removePolicy = (index: number) => {
    const gone = rows[index].policy.name;
    const stripped = rows
      .filter((_, i) => i !== index)
      .map((row) => ({
        ...row,
        cells: new Map(
          [...row.cells.entries()].map(([profileId, egress]) => [
            profileId,
            {
              ...egress,
              members: egress.members.filter(
                (m) => !(m.kind === "policy" && m.ref === gone),
              ),
            },
          ]),
        ),
      }));
    patchRows(stripped);
  };

  const openRuleSetEditor = (index: number) => {
    const target = cloneRuleSet(ruleSets[index]);
    setRuleSetEditor({
      index,
      initial: cloneRuleSet(target),
      draft: target,
    });
  };

  const openAddRuleSet = () => {
    let name = "新规则集";
    for (let i = 2; ruleSets.some((set) => set.name === name); i++)
      name = `新规则集 ${i}`;
    const blank: RuleSet = { id: 0, name, payload: "", remark: "" };
    setRuleSetEditor({
      index: null,
      initial: cloneRuleSet(blank),
      draft: blank,
    });
  };

  /** RULE-SET rules name a set by bare name, so the draft rows follow a rename. */
  const confirmRuleSetEditor = (nextSet: RuleSet) => {
    if (!ruleSetEditor) return;
    const normalized: RuleSet = { ...cloneRuleSet(nextSet), name: nextSet.name.trim() };
    if (ruleSetEditor.index === null) {
      setRuleSets([...ruleSets, normalized]);
      setDirty(true);
    } else {
      const idx = ruleSetEditor.index;
      const previous = ruleSets[idx].name;
      setRuleSets(ruleSets.map((set, i) => (i === idx ? normalized : set)));
      if (previous !== normalized.name) {
        setRows(renameRuleSetReferences(rows, previous, normalized.name));
      }
      setDirty(true);
    }
    setRuleSetEditor(null);
  };

  const removeRuleSet = (index: number) => {
    const gone = ruleSets[index].name;
    setRuleSets(ruleSets.filter((_, i) => i !== index));
    patchRows(dropRuleSetReferences(rows, gone));
  };

  const closeProfileDraft = useCallback(() => {
    setProfileDraft(null);
    setInitialProfileDraft(null);
  }, []);

  const saveProfile = async () => {
    if (!profileDraft?.name.trim() || profileSaving) return;
    setProfileSaving(true);
    const body = { ...profileDraft, name: profileDraft.name.trim() };
    try {
      if (profileDraft.id) {
        const result = await api.put<{ syncError: string }>(
          `/profiles/${profileDraft.id}`,
          body,
        );
        toast.success(
          result.syncError ? "方案已保存" : "方案已保存，相关用户已同步到面板",
        );
        reportSyncError(result.syncError);
      } else {
        await api.post("/profiles", body);
        toast.success("方案已创建");
      }
      closeProfileDraft();
      await reloadProfiles();
    } catch (err) {
      toast.error(errorMessage(err, "保存失败"));
    } finally {
      setProfileSaving(false);
    }
  };

  const removeProfile = async (profile: Profile) => {
    try {
      await api.del(`/profiles/${profile.id}`);
      toast.success("方案已删除");
      // The column's overrides are gone server-side; drop them from the draft
      // too so a later save does not resurrect them.
      setRows(
        rows.map((row) => {
          const cells = new Map(row.cells);
          cells.delete(profile.id);
          return { ...row, cells };
        }),
      );
      await reloadProfiles();
    } catch (err) {
      toast.error(errorMessage(err, "删除失败"));
      throw err;
    }
  };

  const applyPreset = async (preset: RoutingPreset) => {
    try {
      await api.post(`/routing/presets/${preset.key}`);
    } catch (err) {
      toast.error(errorMessage(err, "套用预设失败"));
      throw err;
    }
    toast.success(`已套用「${preset.name}」分流`);
    if (!(await load())) toast.warning("已套用预设，但刷新失败，请重新加载");
  };

  const retry = () => {
    setRetrying(true);
    void load().finally(() => setRetrying(false));
  };

  const refreshProfiles = () => {
    setRefreshingProfiles(true);
    void reloadProfiles().finally(() => setRefreshingProfiles(false));
  };

  const openNewProfile = () => {
    const next: ProfileDraft = {
      name: "",
      allGroups: false,
      enabled: true,
      remark: "",
      groupIds: [],
    };
    setInitialProfileDraft({ ...next, groupIds: [] });
    setProfileDraft(next);
  };

  const openEditProfile = (profile: Profile) => {
    const next: ProfileDraft = {
      id: profile.id,
      name: profile.name,
      allGroups: profile.allGroups,
      enabled: profile.enabled,
      remark: profile.remark,
      groupIds: [...profile.groupIds],
    };
    setInitialProfileDraft({ ...next, groupIds: [...profile.groupIds] });
    setProfileDraft(next);
  };

  if (loading || !options) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="分流"
          description="行是分流策略，列是分流方案，格子里是出口。"
        />
        {loadError ? (
          <LoadErrorAlert
            title="无法加载分流配置"
            message={loadError}
            retrying={retrying}
            onRetry={retry}
          />
        ) : (
          <>
            <Card>
              <CardHeader>
                <CardTitle>分流方案</CardTitle>
              </CardHeader>
              <CardContent className="px-0">
                <TableSkeleton
                  rows={2}
                  columns={[
                    { label: "方案", width: "w-24" },
                    { label: "可用节点组", width: "w-16" },
                    { label: "可用入站", className: "hidden md:table-cell", width: "w-10" },
                    { label: "套餐", className: "hidden md:table-cell", width: "w-10" },
                    { width: "w-24" },
                  ]}
                />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <CardTitle>分流矩阵</CardTitle>
              </CardHeader>
              <CardContent className="px-0">
                <TableSkeleton
                  columns={[
                    { label: "分流策略", width: "w-28" },
                    { label: "默认出口", width: "w-40" },
                    { width: "w-16" },
                  ]}
                />
              </CardContent>
            </Card>
          </>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-6" aria-busy={saving} inert={saving}>
      <PageHeader
        title="分流"
        description="行是分流策略（一份规则清单），列是分流方案。第一列是默认出口，方案列只写和默认不同的部分。"
      >
        {readOnly && (
          <Badge variant="outline">
            <EyeIcon data-icon="inline-start" />
            只读
          </Badge>
        )}
        {!readOnly && options.presets.length > 0 && (
          <DropdownMenu>
            <DropdownMenuTrigger render={<Button variant="outline" />}>
              <LayoutTemplateIcon data-icon="inline-start" />
              套用预设
              <ChevronDownIcon data-icon="inline-end" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-80">
              <DropdownMenuGroup>
                <DropdownMenuLabel>内置分流预设</DropdownMenuLabel>
                {options.presets.map((preset) => (
                  <DropdownMenuItem
                    key={preset.key}
                    className="flex-col items-start gap-0.5"
                    onClick={() => setPendingPreset(preset)}
                  >
                    <span className="font-medium">{preset.name}</span>
                    <span className="text-xs whitespace-normal text-muted-foreground">
                      {preset.description}
                    </span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        {!readOnly && (
          <>
            <Button variant="outline" onClick={openAddPolicy}>
              <PlusIcon data-icon="inline-start" />
              新建策略
            </Button>
            <Button disabled={saving || !dirty} onClick={() => void save()}>
              {saving ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <SaveIcon data-icon="inline-start" />
              )}
              {saving ? "保存中…" : dirty ? "保存分流" : "已保存"}
            </Button>
          </>
        )}
      </PageHeader>

      {needsReview && (
        <Alert>
          <TriangleAlertIcon />
          <AlertTitle>这份分流是从旧版本迁移过来的，请复核一遍</AlertTitle>
          <AlertDescription>
            旧的规则清单已按目标拆成了分流策略，顺序按每条策略首条规则的位置排列，可能和你原来的客户端排列不同；
            每个套餐也各自生成了一个分流方案。确认无误后保存一次，这条提示就会消失。
          </AlertDescription>
          {!readOnly && (
            <AlertAction>
              <Button
                variant="outline"
                size="sm"
                disabled={saving}
                onClick={() => void save()}
              >
                {saving ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <SaveIcon data-icon="inline-start" />
                )}
                {saving ? "保存中…" : "确认并保存"}
              </Button>
            </AlertAction>
          )}
        </Alert>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2">
            <span>分流方案</span>
            <Badge variant="secondary">即时生效</Badge>
          </CardTitle>
          <CardDescription>
            方案的「可用节点组」决定它的用户能连上哪些入站——这是唯一会写到面板的设置。套餐绑方案，用户绑套餐。
          </CardDescription>
          <CardAction className="flex gap-2">
            <RefreshButton
              loading={refreshingProfiles}
              onClick={refreshProfiles}
            />
            {!readOnly && (
              <Button variant="outline" size="sm" onClick={openNewProfile}>
                <PlusIcon data-icon="inline-start" />
                新建方案
              </Button>
            )}
          </CardAction>
        </CardHeader>
        <CardContent className="px-0">
          {profiles.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ListTreeIcon />
                </EmptyMedia>
                <EmptyTitle>还没有分流方案</EmptyTitle>
                <EmptyDescription>
                  没有方案，套餐就无处可绑，用户也拿不到任何节点。
                </EmptyDescription>
              </EmptyHeader>
              {!readOnly && (
                <EmptyContent>
                  <Button onClick={openNewProfile}>
                    <PlusIcon data-icon="inline-start" />
                    新建方案
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>方案</TableHead>
                  <TableHead>可用节点组</TableHead>
                  <TableHead className="hidden md:table-cell">
                    可用入站
                  </TableHead>
                  <TableHead className="hidden md:table-cell">套餐</TableHead>
                  {!readOnly && (
                    <TableHead>
                      <span className="sr-only">操作</span>
                    </TableHead>
                  )}
                </TableRow>
              </TableHeader>
              <TableBody>
                {profiles.map((profile) => (
                  <TableRow key={profile.id}>
                    <TableCell className="min-w-40 max-w-80 whitespace-normal [overflow-wrap:anywhere]">
                      <div className="flex flex-col gap-1">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="font-medium">{profile.name}</span>
                          {!profile.enabled && (
                            <StatusBadge tone="idle">停用</StatusBadge>
                          )}
                        </div>
                        {profile.remark && (
                          <span className="text-xs text-muted-foreground">
                            {profile.remark}
                          </span>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {profile.allGroups ? (
                        <Badge variant="secondary">全部节点组</Badge>
                      ) : profile.groupIds.length === 0 ? (
                        <StatusBadge tone="warn">未选任何分组</StatusBadge>
                      ) : (
                        <span className="tabular-nums">
                          {profile.groupIds.length}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">
                      {profile.usableInbounds}
                    </TableCell>
                    <TableCell className="hidden tabular-nums md:table-cell">
                      {profile.planCount}
                    </TableCell>
                    {!readOnly && (
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            variant="outline"
                            size="sm"
                            aria-label={`编辑方案 ${profile.name}`}
                            onClick={() => openEditProfile(profile)}
                          >
                            <PencilIcon data-icon="inline-start" />
                            <span className="hidden sm:inline">编辑</span>
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            aria-label={`删除方案 ${profile.name}`}
                            onClick={() => setPendingProfileDelete(profile)}
                          >
                            <Trash2Icon data-icon="inline-start" />
                            <span className="hidden sm:inline">删除</span>
                          </Button>
                        </div>
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2">
            <span>分流矩阵</span>
            <Badge variant="outline">草稿统一保存</Badge>
          </CardTitle>
          <CardDescription>
            行的先后既是规则的匹配顺序，也是客户端里策略组的排列顺序。点行首编规则清单，点格子改出口，可拖拽或用右侧箭头调整顺序。
          </CardDescription>
          {dirty && (
            <CardAction>
              <Badge variant="secondary">有未保存的改动</Badge>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="px-0">
          {rows.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <RouteIcon />
                </EmptyMedia>
                <EmptyTitle>还没有分流策略</EmptyTitle>
                <EmptyDescription>
                  策略是一份规则清单，每个方案在矩阵里为它选一个出口。
                </EmptyDescription>
              </EmptyHeader>
              {!readOnly && (
                <EmptyContent>
                  <Button onClick={openAddPolicy}>
                    <PlusIcon data-icon="inline-start" />
                    新建策略
                  </Button>
                </EmptyContent>
              )}
            </Empty>
          ) : (
          <Table
            containerProps={{
              role: "region",
              "aria-label": "分流矩阵，可横向滚动",
              tabIndex: 0,
              onScroll: (event) => {
                const { scrollLeft } = event.currentTarget;
                // Ignore tiny trackpad movements near the start of the table.
                setMatrixScrolled((compact) => scrollLeft > (compact ? 4 : 24));
              },
              className: cn(
                "isolate [--policy-width:7rem] [--default-expanded-width:8rem] [--default-width:var(--default-expanded-width)] [--actions-width:3rem] sm:[--policy-width:14rem] sm:[--default-expanded-width:20rem] sm:[--actions-width:7rem]",
                matrixScrolled &&
                  "[--default-width:6rem] sm:[--default-width:10rem]",
              ),
            }}
            className="table-fixed min-w-full"
            // Keep the scroll range stable when the default column shrinks;
            // the spacer before the fixed actions absorbs the released space.
            style={{
              width: `calc(var(--policy-width) + var(--default-expanded-width) + ${profiles.length * 16}rem${readOnly ? "" : " + var(--actions-width)"})`,
            }}
          >
            <TableHeader>
              <TableRow>
                <TableHead className="sticky left-0 z-20 w-(--policy-width) bg-card">
                  分流策略
                </TableHead>
                <TableHead
                  className={cn(
                    "sticky left-(--policy-width) z-10 w-(--default-width) border-r bg-card transition-[width] duration-300 ease-out motion-reduce:transition-none",
                    stickyEdgeShadow,
                    matrixScrolled && "after:opacity-100",
                  )}
                >
                  默认出口
                </TableHead>
                {profiles.map((profile) => (
                  <TableHead key={profile.id} className="w-64">
                    <span className="block truncate" title={profile.name}>
                      {profile.name}
                    </span>
                  </TableHead>
                ))}
                <TableHead aria-hidden="true" className="p-0" />
                {!readOnly && (
                  <TableHead className="sticky right-0 z-20 w-(--actions-width) border-l bg-card">
                    <span className="sr-only">顺序与操作</span>
                  </TableHead>
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row, index) => (
                <TableRow
                  key={row._key ?? `row-${index}`}
                  data-disabled={!row.policy.enabled || undefined}
                  className={cn(
                    dragOverRowIndex === index &&
                      dragRowIndex !== null &&
                      dragRowIndex !== index &&
                      "bg-primary/5 outline-1 outline-primary/40",
                  )}
                  onDragOver={(event) => {
                    if (readOnly || dragRowIndex === null) return;
                    event.preventDefault();
                    event.dataTransfer.dropEffect = "move";
                    if (dragOverRowIndex !== index) setDragOverRowIndex(index);
                  }}
                  onDrop={(event) => {
                    if (readOnly || dragRowIndex === null) return;
                    event.preventDefault();
                    if (dragRowIndex !== index) {
                      patchRows(reorder(rows, dragRowIndex, index));
                    }
                    setDragRowIndex(null);
                    setDragOverRowIndex(null);
                  }}
                >
                  <TableCell className="sticky left-0 z-20 bg-card">
                    <div className="flex items-start gap-1">
                      {!readOnly && (
                        <span
                          draggable
                          title="拖拽调整策略顺序"
                          aria-label={`拖拽调整 ${row.policy.name} 的顺序`}
                          onDragStart={(event) => {
                            setDragRowIndex(index);
                            event.dataTransfer.effectAllowed = "move";
                            event.dataTransfer.setData("text/plain", String(index));
                          }}
                          onDragEnd={() => {
                            setDragRowIndex(null);
                            setDragOverRowIndex(null);
                          }}
                          className="mt-0.5 hidden shrink-0 cursor-grab rounded p-0.5 text-muted-foreground/70 hover:text-foreground active:cursor-grabbing sm:inline-flex"
                        >
                          <GripVerticalIcon className="size-4" />
                        </span>
                      )}
                      <button
                        type="button"
                        className="flex w-full min-w-0 flex-col items-start gap-1 rounded-md text-left focus-visible:outline-ring"
                        title={`${row.policy.name} · ${ruleCountLabel(row.policy.rules)}`}
                        onClick={() => openPolicyEditor(index)}
                      >
                        <span className="flex w-full items-center gap-2 font-medium">
                          {row.policy.icon && (
                            <span className="shrink-0">{row.policy.icon}</span>
                          )}
                          <span className="truncate">{row.policy.name}</span>
                          {row.policy.isFinal && (
                            <Badge variant="outline">兜底</Badge>
                          )}
                          {!row.policy.enabled && (
                            <StatusBadge tone="idle">停用</StatusBadge>
                          )}
                        </span>
                        <span className="w-full truncate text-xs text-muted-foreground">
                          {ruleCountLabel(row.policy.rules)}
                        </span>
                      </button>
                    </div>
                  </TableCell>

                  <CellButton
                    className={cn(
                      "sticky left-(--policy-width) z-10 border-r bg-card",
                      stickyEdgeShadow,
                      matrixScrolled && "after:opacity-100",
                    )}
                    compact={matrixScrolled}
                    label={`${row.policy.name} · 默认出口`}
                    summary={cellSummary(
                      effective(row, DEFAULT_COLUMN),
                      memberLabel,
                      false,
                    )}
                    onClick={() =>
                      openCellEditor(index, DEFAULT_COLUMN)
                    }
                  />

                  {profiles.map((profile) => (
                    <CellButton
                      key={profile.id}
                      label={`${row.policy.name} · ${profile.name}`}
                      summary={cellSummary(
                        effective(row, profile.id),
                        memberLabel,
                        !row.cells.has(profile.id),
                      )}
                      onClick={() =>
                        openCellEditor(index, profile.id)
                      }
                    />
                  ))}

                  <TableCell aria-hidden="true" className="p-0" />
                  {!readOnly && (
                    <TableCell className="sticky right-0 z-20 border-l bg-card">
                      <div className="flex flex-col items-center justify-end gap-1 sm:flex-row">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`上移 ${row.policy.name}`}
                          disabled={index === 0}
                          onClick={() => patchRows(move(rows, index, -1))}
                        >
                          <ArrowUpIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`下移 ${row.policy.name}`}
                          disabled={index === rows.length - 1}
                          onClick={() => patchRows(move(rows, index, 1))}
                        >
                          <ArrowDownIcon />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={`删除策略 ${row.policy.name}`}
                          onClick={() => setPendingPolicyDelete(index)}
                        >
                          <Trash2Icon />
                        </Button>
                      </div>
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center gap-2">
            <span>规则集</span>
            <Badge variant="outline">草稿统一保存</Badge>
          </CardTitle>
          <CardDescription>
            长的域名清单放在这里，策略里用一条「RULE-SET,规则集名称」引用。客户端每 6 小时自动拉取最新内容，套用预设也不会覆盖已有的同名规则集。和矩阵一起保存。
          </CardDescription>
          {!readOnly && (
            <CardAction>
              <Button variant="outline" size="sm" onClick={openAddRuleSet}>
                <PlusIcon data-icon="inline-start" />
                新建规则集
              </Button>
            </CardAction>
          )}
        </CardHeader>
        <CardContent className="px-0">
          {ruleSets.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <FileTextIcon />
                </EmptyMedia>
                <EmptyTitle>还没有规则集</EmptyTitle>
                <EmptyDescription>
                  规则不多时直接写在策略里就够了；清单很长或经常增删时再建一个规则集。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>规则集</TableHead>
                  <TableHead>规则数</TableHead>
                  <TableHead className="hidden md:table-cell">
                    引用它的策略
                  </TableHead>
                  <TableHead>
                    <span className="sr-only">操作</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {ruleSets.map((set, index) => {
                  const users = ruleSetUsers(rows, set.name);
                  return (
                    <TableRow key={set.id || `new-${index}`}>
                      <TableCell className="min-w-40 max-w-80 whitespace-normal [overflow-wrap:anywhere]">
                        <div className="flex flex-col gap-1">
                          <span className="font-medium">{set.name}</span>
                          {set.remark && (
                            <span className="text-xs text-muted-foreground">
                              {set.remark}
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="tabular-nums">
                        {ruleSetLineCount(set.payload)}
                      </TableCell>
                      <TableCell className="hidden whitespace-normal md:table-cell">
                        {users.length === 0 ? (
                          <StatusBadge tone="idle">未被引用</StatusBadge>
                        ) : (
                          <div className="flex flex-wrap gap-1">
                            {users.map((name) => (
                              <Badge key={name} variant="secondary">
                                {name}
                              </Badge>
                            ))}
                          </div>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-1">
                          <Button
                            variant="outline"
                            size="sm"
                            aria-label={`${readOnly ? "查看" : "编辑"}规则集 ${set.name}`}
                            onClick={() => openRuleSetEditor(index)}
                          >
                            {readOnly ? (
                              <EyeIcon data-icon="inline-start" />
                            ) : (
                              <PencilIcon data-icon="inline-start" />
                            )}
                            <span className="hidden sm:inline">
                              {readOnly ? "查看" : "编辑"}
                            </span>
                          </Button>
                          {!readOnly && (
                            <Button
                              variant="ghost"
                              size="sm"
                              aria-label={`删除规则集 ${set.name}`}
                              onClick={() => setPendingRuleSetDelete(index)}
                            >
                              <Trash2Icon data-icon="inline-start" />
                              <span className="hidden sm:inline">删除</span>
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>渲染预览</CardTitle>
          <CardDescription>
            按已保存的配置渲染一份完整配置。代理字段是占位，实际值在订阅时从面板取回，但分组和规则就是客户端会拿到的。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-2">
            <Select
              disabled={previewing}
              items={[
                {
                  value: String(DEFAULT_COLUMN),
                  label: "默认出口（按全部节点组）",
                },
                ...profiles.map((p) => ({
                  value: String(p.id),
                  label: p.name,
                })),
              ]}
              value={String(previewProfile)}
              onValueChange={(value) => {
                setPreviewProfile(Number(value));
                setPreview(null);
              }}
            >
              <SelectTrigger aria-label="预览哪个方案" className="w-full sm:w-56">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value={String(DEFAULT_COLUMN)}>
                    默认出口（按全部节点组）
                  </SelectItem>
                  {profiles.map((profile) => (
                    <SelectItem key={profile.id} value={String(profile.id)}>
                      {profile.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <Button
              variant="outline"
              size="sm"
              disabled={previewing}
              onClick={() => void runPreview()}
            >
              {previewing ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <EyeIcon data-icon="inline-start" />
              )}
              {previewing ? "生成中…" : "生成预览"}
            </Button>
          </div>
          {dirty && (
            <Alert variant="warning">
              <TriangleAlertIcon />
              <AlertTitle>预览用的是已保存的配置</AlertTitle>
              <AlertDescription>
                当前还有未保存的改动，先保存再预览才能看到它们。
              </AlertDescription>
              {!readOnly && (
                <AlertAction>
                  <Button
                    type="button"
                    size="sm"
                    disabled={saving || previewing}
                    onClick={() => void saveAndPreview()}
                  >
                    {saving ? (
                      <Spinner data-icon="inline-start" />
                    ) : (
                      <SaveIcon data-icon="inline-start" />
                    )}
                    保存并预览
                  </Button>
                </AlertAction>
              )}
            </Alert>
          )}
          {preview?.error && (
            <Alert variant="destructive">
              <TriangleAlertIcon />
              <AlertTitle>配置有问题</AlertTitle>
              <AlertDescription>{preview.error}</AlertDescription>
            </Alert>
          )}
          {preview?.yaml && (
            <CodeBlock className="max-h-[60vh]">{preview.yaml}</CodeBlock>
          )}
          {previewing && (
            <Skeleton className="h-64 w-full" aria-label="正在生成预览" />
          )}
          {!preview && !previewing && (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <EyeIcon />
                </EmptyMedia>
                <EmptyTitle>还没有生成预览</EmptyTitle>
                <EmptyDescription>
                  选择一个方案，再点「生成预览」。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
        </CardContent>
      </Card>

      <StickySaveBar
        visible={!readOnly && dirty}
        saving={saving}
        message="分流矩阵或规则集有未保存的改动"
        saveLabel="保存分流"
        savingLabel="保存中…"
        onDiscard={() => setConfirmDiscard(true)}
        onSave={() => void save()}
      />

      <ConfirmDialog
        open={confirmDiscard}
        onOpenChange={setConfirmDiscard}
        title="放弃分流矩阵和规则集的未保存改动？"
        description="会重新从服务端加载最近一次已保存的分流策略与规则集，不影响已保存的分流方案。"
        confirmLabel="放弃改动"
        onConfirm={async () => {
          if (!(await load())) throw new Error("重新加载失败");
          toast.info("已恢复为上次保存的配置");
        }}
      />

      <RuleSetDialog
        readOnly={readOnly}
        editor={ruleSetEditor}
        nameTaken={(name) =>
          ruleSets.some((set, i) => i !== ruleSetEditor?.index && set.name === name)
        }
        onChange={(draft) =>
          setRuleSetEditor((current) => (current ? { ...current, draft } : null))
        }
        onClose={() => setRuleSetEditor(null)}
        onConfirm={confirmRuleSetEditor}
      />

      <ConfirmDialog
        open={pendingRuleSetDelete !== null}
        onOpenChange={(open) => !open && setPendingRuleSetDelete(null)}
        title={`删除规则集「${pendingRuleSetDelete !== null ? (ruleSets[pendingRuleSetDelete]?.name ?? "") : ""}」？`}
        description={(() => {
          const users =
            pendingRuleSetDelete !== null && ruleSets[pendingRuleSetDelete]
              ? ruleSetUsers(rows, ruleSets[pendingRuleSetDelete].name)
              : [];
          return users.length > 0
            ? `「${users.join("」「")}」里引用它的 RULE-SET 规则会一并移除，这些流量会落到后面的规则。保存后才会真正生效。`
            : "保存后才会真正生效。";
        })()}
        confirmLabel="删除规则集"
        onConfirm={async () => {
          if (pendingRuleSetDelete !== null) removeRuleSet(pendingRuleSetDelete);
          setPendingRuleSetDelete(null);
        }}
      />

      <PolicyDialog
        readOnly={readOnly}
        editor={policyEditor}
        options={options}
        ruleSetNames={ruleSets.map((set) => set.name)}
        nameTaken={(name) =>
          rows.some((row, i) => i !== policyEditor?.index && row.policy.name === name)
        }
        onChange={(draft) =>
          setPolicyEditor((current) => (current ? { ...current, draft } : null))
        }
        onClose={() => setPolicyEditor(null)}
        onConfirm={confirmPolicyEditor}
      />

      <EgressDialog
        readOnly={readOnly}
        open={cellEditor !== null}
        profileName={
          cellEditor?.profileId === DEFAULT_COLUMN
            ? "默认"
            : (profiles.find((p) => p.id === cellEditor?.profileId)?.name ?? "")
        }
        policyName={cellEditor ? rows[cellEditor.row].policy.name : ""}
        isFinal={cellEditor ? rows[cellEditor.row].policy.isFinal : false}
        isDefaultColumn={cellEditor?.profileId === DEFAULT_COLUMN}
        initial={cellEditor?.initial ?? null}
        own={cellEditor?.draft ?? null}
        inherited={
          cellEditor && cellEditor.profileId !== DEFAULT_COLUMN
            ? (rows[cellEditor.row].cells.get(DEFAULT_COLUMN) ?? null)
            : null
        }
        options={options}
        groups={groups}
        policies={rows.map((row) => row.policy)}
        memberLabel={memberLabel}
        memberMissing={memberMissing}
        onClose={() => setCellEditor(null)}
        onChange={(egress) => {
          setCellEditor((editor) => (editor ? { ...editor, draft: egress } : null));
        }}
        onConfirm={() => {
          if (!cellEditor) return;
          setCell(cellEditor.row, cellEditor.profileId, cellEditor.draft);
          setCellEditor(null);
        }}
      />

      <ProfileDialog
        saving={profileSaving}
        initialDraft={initialProfileDraft}
        draft={profileDraft}
        groups={groups}
        onChange={setProfileDraft}
        onClose={closeProfileDraft}
        onSave={() => void saveProfile()}
      />

      <ConfirmDialog
        open={pendingProfileDelete !== null}
        onOpenChange={(open) => !open && setPendingProfileDelete(null)}
        title={`删除方案「${pendingProfileDelete?.name ?? ""}」？`}
        description="节点组和策略都不受影响，但这个方案那一列的出口覆盖会一并删除。绑定了它的套餐必须先改掉。"
        confirmLabel="删除方案"
        onConfirm={async () => {
          if (pendingProfileDelete) await removeProfile(pendingProfileDelete);
        }}
      />

      <ConfirmDialog
        open={pendingPreset !== null}
        onOpenChange={(open) => !open && setPendingPreset(null)}
        title={`套用「${pendingPreset?.name ?? ""}」分流预设？`}
        description={
          <>
            现有的分流策略和规则会整体替换成预设，并立即保存。方案和可用节点组不变；方案列里同名策略的出口覆盖会保留，其余覆盖丢弃。
            {dirty && " 矩阵里尚未保存的改动也会一并丢弃。"}
          </>
        }
        confirmLabel="套用预设"
        onConfirm={async () => {
          if (pendingPreset) await applyPreset(pendingPreset);
        }}
      />

      <ConfirmDialog
        open={pendingPolicyDelete !== null}
        onOpenChange={(open) => !open && setPendingPolicyDelete(null)}
        title={`删除策略「${pendingPolicyDelete !== null ? rows[pendingPolicyDelete]?.policy.name : ""}」？`}
        description="它的规则清单、每一列的出口，以及其他策略里对它的引用都会一并移除。保存后才会真正生效。"
        confirmLabel="删除策略"
        onConfirm={async () => {
          if (pendingPolicyDelete !== null) removePolicy(pendingPolicyDelete);
          setPendingPolicyDelete(null);
        }}
      />
    </div>
  );
}

/** A soft edge on the pinned default column, shown while profile columns scroll under it. */
const stickyEdgeShadow =
  "after:pointer-events-none after:absolute after:inset-y-0 after:left-full after:w-3 after:bg-linear-to-r after:from-foreground/6 after:to-transparent after:opacity-0 after:transition-opacity after:duration-300 motion-reduce:after:transition-none dark:after:from-black/50";

function ruleCountLabel(rules: Policy["rules"]): string {
  if (rules.length === 0) return "无规则（纯选择器）";
  const off = rules.filter((rule) => rule.disabled).length;
  return off > 0
    ? `${rules.length} 条规则，${off} 条已停用`
    : `${rules.length} 条规则`;
}

/** cellSummary renders what a column actually does, inheritance included. */
function cellSummary(
  resolved: { egress: Egress; inherited: boolean } | null,
  memberLabel: (member: EgressMember) => string,
  inheriting: boolean,
): {
  text: string;
  compactText?: string;
  tone: "normal" | "muted" | "hidden";
} {
  if (!resolved) return { text: "未配置", tone: "hidden" };
  if (resolved.egress.hidden) return { text: "不下发", tone: "hidden" };
  const names = resolved.egress.members.map(memberLabel);
  const text = names.length === 0 ? "（空）" : names.join(" · ");
  return {
    text: `${resolved.egress.clientHidden ? "客户端隐藏 · " : ""}${resolved.egress.type} · ${text}`,
    compactText: `${resolved.egress.clientHidden ? "隐藏 · " : ""}${names[0] ?? "（空）"}${names.length > 1 ? ` +${names.length - 1}` : ""}`,
    tone: inheriting ? "muted" : "normal",
  };
}

function CellButton({
  className,
  compact,
  label,
  summary,
  onClick,
}: {
  className?: string;
  compact?: boolean;
  label: string;
  summary: ReturnType<typeof cellSummary>;
  onClick: () => void;
}) {
  return (
    <TableCell className={className}>
      <button
        type="button"
        className={cn(
          "w-full rounded-md px-2 py-1 text-left text-sm transition-[padding] duration-300 ease-out hover:bg-accent focus-visible:outline-ring motion-reduce:transition-none",
          compact && "px-1",
        )}
        aria-label={`${label}：${summary.text}`}
        title={summary.text}
        onClick={onClick}
      >
        <span
          aria-hidden="true"
          className={cn(
            "grid min-w-0",
            summary.tone !== "normal" && "text-muted-foreground",
            summary.tone === "hidden" && "italic",
          )}
        >
          {/* The two labels share one grid cell: the outgoing one clears before
              the incoming one fades in, so they never show through each other. */}
          <span
            className={cn(
              "col-start-1 row-start-1 min-w-0 truncate transition-opacity ease-out motion-reduce:transition-none",
              compact && summary.compactText
                ? "opacity-0 duration-100"
                : "opacity-100 duration-200 delay-100",
            )}
          >
            {summary.text}
          </span>
          {compact !== undefined && summary.compactText && (
            <span
              className={cn(
                "col-start-1 row-start-1 min-w-0 truncate transition-opacity ease-out motion-reduce:transition-none",
                compact
                  ? "opacity-100 duration-200 delay-100"
                  : "opacity-0 duration-100",
              )}
            >
              {summary.compactText}
            </span>
          )}
        </span>
      </button>
    </TableCell>
  );
}

function RuleSetDialog({
  readOnly,
  editor,
  nameTaken,
  onChange,
  onClose,
  onConfirm,
}: {
  readOnly: boolean;
  editor: RuleSetEditorState | null;
  nameTaken: (name: string) => boolean;
  onChange: (draft: RuleSet) => void;
  onClose: () => void;
  onConfirm: (draft: RuleSet) => void;
}) {
  const set = editor?.draft ?? null;
  const dirty =
    !readOnly &&
    editor !== null &&
    JSON.stringify(editor.draft) !== JSON.stringify(editor.initial);
  const onOpenChange = useDirtyDialog(dirty, false, onClose);
  const name = set?.name.trim() ?? "";
  const nameError = !set
    ? ""
    : !name
      ? "请输入规则集名称。"
      : name.includes(",")
        ? "名称不能包含英文逗号。"
        : nameTaken(name)
          ? "已有同名的规则集。"
          : "";
  return (
    <Dialog open={set !== null} onOpenChange={onOpenChange}>
      <DialogContent className="flex flex-col overflow-hidden sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {readOnly ? "查看规则集" : editor?.index === null ? "新建规则集" : "编辑规则集"}
          </DialogTitle>
          <DialogDescription>
            客户端按 mihomo rule-provider 拉取这份清单。命中后走哪个出口，由引用它的策略决定。
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {set && (
            <FieldGroup className="min-w-0">
              <Field data-invalid={nameError ? true : undefined}>
                <FieldLabel htmlFor="rule-set-name">名称</FieldLabel>
                <Input
                  id="rule-set-name"
                  readOnly={readOnly}
                  value={set.name}
                  aria-invalid={nameError ? true : undefined}
                  onChange={(event) => onChange({ ...set, name: event.target.value })}
                />
                {nameError ? (
                  <FieldError>{nameError}</FieldError>
                ) : (
                  <FieldDescription>
                    策略里写「RULE-SET,{name}」引用它。改名时引用它的规则会跟着改。
                  </FieldDescription>
                )}
              </Field>
              <Field>
                <FieldLabel htmlFor="rule-set-payload">
                  规则（{ruleSetLineCount(set.payload)} 条）
                </FieldLabel>
                <CodeTextarea
                  id="rule-set-payload"
                  readOnly={readOnly}
                  rows={14}
                  className="max-h-[50dvh] min-h-48"
                  placeholder={"# Hugging Face\nGEOSITE,huggingface\nDOMAIN-SUFFIX,githubusercontent.com"}
                  value={set.payload}
                  onChange={(event) => onChange({ ...set, payload: event.target.value })}
                />
                <FieldDescription>
                  每行一条「类型,内容」，不写策略名；IP 类规则可在末尾加 ,no-resolve。# 开头的行是注释。不能再嵌套 RULE-SET。保存时会逐行检查。
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="rule-set-remark">备注</FieldLabel>
                <Input
                  id="rule-set-remark"
                  readOnly={readOnly}
                  value={set.remark}
                  onChange={(event) => onChange({ ...set, remark: event.target.value })}
                />
              </Field>
            </FieldGroup>
          )}
        </DialogBody>
        <DialogFooter>
          {readOnly ? (
            <Button onClick={onClose}>关闭</Button>
          ) : (
            <>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button
                type="button"
                disabled={!set || Boolean(nameError)}
                onClick={() => set && onConfirm(set)}
              >
                确定
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PolicyDialog({
  readOnly,
  editor,
  options,
  ruleSetNames,
  nameTaken,
  onChange,
  onClose,
  onConfirm,
}: {
  readOnly: boolean;
  editor: PolicyEditorState | null;
  options: RoutingMatrix["options"];
  ruleSetNames: string[];
  nameTaken: (name: string) => boolean;
  onChange: (policy: Policy) => void;
  onClose: () => void;
  onConfirm: (policy: Policy) => void;
}) {
  const [dragRuleIndex, setDragRuleIndex] = useState<number | null>(null);
  const [dragOverRuleIndex, setDragOverRuleIndex] = useState<number | null>(null);
  const policy = editor?.draft;
  const isOriginalFinal = Boolean(editor?.initial.isFinal);
  const dirty =
    !readOnly &&
    editor !== null &&
    JSON.stringify(editor.draft) !== JSON.stringify(editor.initial);
  const onOpenChange = useDirtyDialog(dirty, false, onClose);
  useEffect(() => {
    if (!editor) {
      setDragRuleIndex(null);
      setDragOverRuleIndex(null);
    }
  }, [editor]);
  const trimmedName = policy?.name.trim() ?? "";
  const nameError = !policy
    ? ""
    : !trimmedName
      ? "请输入策略名称。"
      : policy.name.includes(",")
        ? "名称不能包含英文逗号。"
        : nameTaken(trimmedName)
          ? "已有同名的分流策略。"
          : "";
  return (
    <Dialog open={policy !== undefined} onOpenChange={onOpenChange}>
      <DialogContent className="flex flex-col overflow-hidden sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {readOnly ? "查看分流策略" : editor?.index === null ? "新建分流策略" : "编辑分流策略"}
          </DialogTitle>
          <DialogDescription>
            这份规则清单对所有方案都一样；每个方案走哪条线路，由矩阵里的出口决定。
          </DialogDescription>
        </DialogHeader>
        <DialogBody>
          {policy && (
            <FieldGroup className="min-w-0">
              <FieldGroup className="grid gap-4 sm:grid-cols-[6rem_1fr]">
                <Field>
                  <FieldLabel htmlFor="policy-icon">图标</FieldLabel>
                  <Input
                    id="policy-icon"
                    readOnly={readOnly}
                    placeholder="🤖"
                    value={policy.icon}
                    onChange={(event) =>
                      onChange({ ...policy, icon: event.target.value })
                    }
                  />
                </Field>
                <Field data-invalid={nameError ? true : undefined}>
                  <FieldLabel htmlFor="policy-name">名称</FieldLabel>
                  <Input
                    id="policy-name"
                    readOnly={readOnly}
                    value={policy.name}
                    aria-invalid={nameError ? true : undefined}
                    onChange={(event) =>
                      onChange({ ...policy, name: event.target.value })
                    }
                  />
                  {nameError ? (
                    <FieldError>{nameError}</FieldError>
                  ) : (
                    <FieldDescription>
                      客户端里的策略组名称就是「图标 + 名称」。
                    </FieldDescription>
                  )}
                </Field>
              </FieldGroup>

              <FieldGroup className="grid gap-4 sm:grid-cols-2">
                <Field orientation="horizontal">
                  <FieldLabel htmlFor="policy-enabled">启用</FieldLabel>
                  <Switch
                    id="policy-enabled"
                    readOnly={readOnly}
                    checked={policy.enabled}
                    onCheckedChange={(enabled) =>
                      onChange({ ...policy, enabled })
                    }
                  />
                </Field>
                <Field>
                  <Field orientation="horizontal">
                    <FieldLabel htmlFor="policy-final">
                      作为兜底（MATCH）
                    </FieldLabel>
                    <Switch
                      id="policy-final"
                      readOnly={readOnly}
                      disabled={readOnly || isOriginalFinal}
                      checked={policy.isFinal}
                      onCheckedChange={(checked) =>
                        onChange({ ...policy, isFinal: Boolean(checked) })
                      }
                    />
                  </Field>
                  {policy.isFinal && (
                    <FieldDescription>
                      当前已是兜底策略，请到其他策略中开启以切换。
                    </FieldDescription>
                  )}
                </Field>
              </FieldGroup>

              <FieldSet className="min-w-0">
                <FieldLegend variant="label">
                  规则清单（{policy.rules.length} 条）
                </FieldLegend>
                <FieldDescription>
                  自上而下匹配。关掉开关的规则会保留但不下发。清单为空也没关系——那就是个纯粹给用户手切的选择器。长清单可以放进规则集，用 RULE-SET 引用。
                </FieldDescription>
                <datalist id="policy-rule-set-names">
                  {ruleSetNames.map((name) => (
                    <option key={name} value={name} />
                  ))}
                </datalist>
                <ScrollArea className="h-72 max-h-[40dvh] min-w-0 rounded-lg border">
                  <div className="flex flex-col gap-2 p-3">
                    {policy.rules.map((rule, index) => (
                      <div
                        key={index}
                        className={cn(
                          "grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-2 rounded-md p-1 sm:grid-cols-[auto_11rem_minmax(0,1fr)_auto_auto]",
                          dragOverRuleIndex === index &&
                            dragRuleIndex !== null &&
                            dragRuleIndex !== index &&
                            "bg-primary/5 ring-1 ring-primary/40",
                        )}
                        data-disabled={rule.disabled || undefined}
                        onDragOver={(event) => {
                          if (readOnly || dragRuleIndex === null) return;
                          event.preventDefault();
                          event.dataTransfer.dropEffect = "move";
                          if (dragOverRuleIndex !== index) setDragOverRuleIndex(index);
                        }}
                        onDrop={(event) => {
                          if (readOnly || dragRuleIndex === null) return;
                          event.preventDefault();
                          if (dragRuleIndex !== index) {
                            onChange({
                              ...policy,
                              rules: reorder(policy.rules, dragRuleIndex, index),
                            });
                          }
                          setDragRuleIndex(null);
                          setDragOverRuleIndex(null);
                        }}
                      >
                        <div className="flex items-center gap-1.5">
                          {!readOnly && (
                            <span
                              draggable
                              title="拖拽调整规则顺序"
                              aria-label={`拖拽调整规则 ${index + 1} 顺序`}
                              onDragStart={(event) => {
                                setDragRuleIndex(index);
                                event.dataTransfer.effectAllowed = "move";
                                event.dataTransfer.setData("text/plain", String(index));
                              }}
                              onDragEnd={() => {
                                setDragRuleIndex(null);
                                setDragOverRuleIndex(null);
                              }}
                              className="hidden shrink-0 cursor-grab rounded p-0.5 text-muted-foreground/70 hover:text-foreground active:cursor-grabbing sm:inline-flex"
                            >
                              <GripVerticalIcon className="size-4" />
                            </span>
                          )}
                          <Switch
                            aria-label={`规则 ${index + 1} ${rule.disabled ? "已停用" : "已启用"}`}
                            readOnly={readOnly}
                            checked={!rule.disabled}
                            onCheckedChange={(enabled) =>
                              onChange({
                                ...policy,
                                rules: policy.rules.map((r, i) =>
                                  i === index ? { ...r, disabled: !enabled } : r,
                                ),
                              })
                            }
                          />
                        </div>
                        <Select
                          readOnly={readOnly}
                          items={options.ruleTypes.map((value) => ({
                            value,
                            label: value,
                          }))}
                          value={rule.type}
                          onValueChange={(value) =>
                            onChange({
                              ...policy,
                              rules: policy.rules.map((r, i) =>
                                i === index ? { ...r, type: String(value) } : r,
                              ),
                            })
                          }
                        >
                          <SelectTrigger
                            className="col-span-2 w-full min-w-0 sm:col-span-1"
                            aria-label={`规则 ${index + 1} 的匹配类型`}
                          >
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectGroup>
                              {options.ruleTypes.map((value) => (
                                <SelectItem key={value} value={value}>
                                  {value}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                        <Input
                          className={cn(
                            "col-span-3 sm:col-span-1",
                            rule.disabled && "line-through opacity-60",
                          )}
                          aria-label={`规则 ${index + 1} 的内容`}
                          readOnly={readOnly}
                          placeholder={
                            rule.type === RULE_SET_TYPE
                              ? "规则集名称"
                              : "例如 openai"
                          }
                          list={
                            rule.type === RULE_SET_TYPE
                              ? "policy-rule-set-names"
                              : undefined
                          }
                          aria-invalid={!rule.value.trim() || undefined}
                          value={rule.value}
                          onChange={(event) =>
                            onChange({
                              ...policy,
                              rules: policy.rules.map((r, i) =>
                                i === index
                                  ? { ...r, value: event.target.value }
                                  : r,
                              ),
                            })
                          }
                        />
                        <Field
                          orientation="horizontal"
                          className="col-span-2 w-auto sm:col-span-1"
                          data-disabled={
                            !options.noResolveTypes[rule.type] || undefined
                          }
                        >
                          <Checkbox
                            id={`policy-rule-${index}-no-resolve`}
                            aria-label={`规则 ${index + 1} 不解析域名`}
                            readOnly={readOnly}
                            disabled={!options.noResolveTypes[rule.type]}
                            checked={
                              rule.noResolve &&
                              Boolean(options.noResolveTypes[rule.type])
                            }
                            onCheckedChange={(checked) =>
                              onChange({
                                ...policy,
                                rules: policy.rules.map((r, i) =>
                                  i === index
                                    ? { ...r, noResolve: Boolean(checked) }
                                    : r,
                                ),
                              })
                            }
                          />
                          <FieldLabel
                            htmlFor={`policy-rule-${index}-no-resolve`}
                            className="font-normal text-muted-foreground"
                          >
                            no-resolve
                          </FieldLabel>
                        </Field>
                        {!readOnly && (
                          <div className="flex items-center gap-1">
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`上移规则 ${index + 1}`}
                              disabled={index === 0}
                              onClick={() =>
                                onChange({
                                  ...policy,
                                  rules: move(policy.rules, index, -1),
                                })
                              }
                            >
                              <ArrowUpIcon />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`下移规则 ${index + 1}`}
                              disabled={index === policy.rules.length - 1}
                              onClick={() =>
                                onChange({
                                  ...policy,
                                  rules: move(policy.rules, index, 1),
                                })
                              }
                            >
                              <ArrowDownIcon />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label={`删除规则 ${index + 1}`}
                              onClick={() =>
                                onChange({
                                  ...policy,
                                  rules: policy.rules.filter((_, i) => i !== index),
                                })
                              }
                            >
                              <Trash2Icon />
                            </Button>
                          </div>
                        )}
                      </div>
                    ))}
                    {!readOnly && (
                      <div>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            onChange({
                              ...policy,
                              rules: [
                                ...policy.rules,
                                {
                                  type: "GEOSITE",
                                  value: "",
                                  noResolve: false,
                                  disabled: false,
                                },
                              ],
                            })
                          }
                        >
                          <PlusIcon data-icon="inline-start" />
                          添加规则
                        </Button>
                      </div>
                    )}
                  </div>
                </ScrollArea>
              </FieldSet>

              <Field>
                <FieldLabel htmlFor="policy-remark">备注</FieldLabel>
                <Input
                  id="policy-remark"
                  readOnly={readOnly}
                  value={policy.remark}
                  onChange={(event) =>
                    onChange({ ...policy, remark: event.target.value })
                  }
                />
              </Field>
            </FieldGroup>
          )}
        </DialogBody>
        <DialogFooter>
          {readOnly ? (
            <Button onClick={onClose}>关闭</Button>
          ) : (
            <>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button
                type="button"
                disabled={!policy || Boolean(nameError)}
                onClick={() => policy && onConfirm(policy)}
              >
                确定
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function EgressDialog({
  readOnly,
  open,
  policyName,
  profileName,
  isFinal,
  isDefaultColumn,
  initial,
  own,
  inherited,
  options,
  groups,
  policies,
  memberLabel,
  memberMissing,
  onClose,
  onChange,
  onConfirm,
}: {
  readOnly: boolean;
  open: boolean;
  policyName: string;
  profileName: string;
  /** The MATCH policy must be emitted in every column, even when hidden in clients. */
  isFinal: boolean;
  isDefaultColumn: boolean;
  initial: Egress | null;
  own: Egress | null;
  inherited: Egress | null;
  options: RoutingMatrix["options"];
  groups: NodeGroup[];
  policies: Policy[];
  memberLabel: (member: EgressMember) => string;
  memberMissing: (member: EgressMember) => boolean;
  onClose: () => void;
  onChange: (egress: Egress | null) => void;
  onConfirm: () => void;
}) {
  const [dragMemberIndex, setDragMemberIndex] = useState<number | null>(null);
  const [dragOverMemberIndex, setDragOverMemberIndex] = useState<number | null>(null);
  useEffect(() => {
    if (!open) {
      setDragMemberIndex(null);
      setDragOverMemberIndex(null);
    }
  }, [open]);

  const dirty =
    !readOnly && open && JSON.stringify(own) !== JSON.stringify(initial);
  const onOpenChange = useDirtyDialog(dirty, false, onClose);

  const mode: "inherit" | "custom" | "hidden" =
    own === null ? "inherit" : own.hidden ? "hidden" : "custom";
  const current = own ?? inherited;

  const setMode = (next: "inherit" | "custom" | "hidden") => {
    if (next === "inherit") return onChange(null);
    if (next === "hidden")
      return onChange({ ...(current ?? blankEgress(0)), hidden: true });
    onChange({ ...(current ?? blankEgress(0)), hidden: false });
  };

  const patch = (values: Partial<Egress>) => {
    if (!current) return;
    onChange({ ...current, ...values, hidden: false });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex flex-col overflow-hidden sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>
            「{policyName}」在{profileName}下的出口
          </DialogTitle>
          <DialogDescription>
            {isDefaultColumn
              ? "默认出口是每个方案的兜底；写「全部节点组」就能让每个方案自动收窄到它自己的白名单。"
              : "只在和默认不同的时候才需要覆盖。"}
          </DialogDescription>
        </DialogHeader>

        <DialogBody>
          <FieldGroup>
            {!isDefaultColumn && (
              <Field>
                <FieldLabel htmlFor="egress-mode">这一格</FieldLabel>
                <Select
                  readOnly={readOnly}
                  items={[
                    { value: "inherit", label: "跟随默认" },
                    { value: "custom", label: "自定义出口" },
                    // A final cell already stored as hidden (from before the
                    // server refused it) still has to display, so it can be
                    // switched back.
                    ...(isFinal && mode !== "hidden"
                      ? []
                      : [
                          {
                            value: "hidden",
                            label: "不下发（这个方案没有这条分流）",
                          },
                        ]),
                  ]}
                  value={mode}
                  onValueChange={(value) =>
                    setMode(value as "inherit" | "custom" | "hidden")
                  }
                >
                  <SelectTrigger id="egress-mode" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="inherit">跟随默认</SelectItem>
                      <SelectItem value="custom">自定义出口</SelectItem>
                      {(!isFinal || mode === "hidden") && (
                        <SelectItem value="hidden">
                          不下发（这个方案没有这条分流）
                        </SelectItem>
                      )}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                {mode === "hidden" && (
                  <FieldDescription>
                    它的策略组和规则都不会下发，这类流量会落到后面的规则或兜底。
                  </FieldDescription>
                )}
                {isFinal && (
                  <FieldDescription>
                    兜底策略每个方案都必须下发，但可以在客户端隐藏。
                  </FieldDescription>
                )}
              </Field>
            )}

            {mode !== "hidden" && current && (
              <>
                <Field orientation="horizontal">
                  <FieldContent>
                    <FieldLabel htmlFor="egress-client-hidden">
                      在客户端隐藏
                    </FieldLabel>
                    <FieldDescription id="egress-client-hidden-description">
                      保留策略组和分流规则，只在支持隐藏的客户端中收起展示。配置文件中仍可查看。
                    </FieldDescription>
                  </FieldContent>
                  <Switch
                    id="egress-client-hidden"
                    readOnly={readOnly}
                    checked={current.clientHidden ?? false}
                    aria-describedby="egress-client-hidden-description"
                    onCheckedChange={(checked) => patch({ clientHidden: checked })}
                  />
                </Field>
                <FieldGroup className="grid gap-4 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="egress-type">选择方式</FieldLabel>
                    <Select
                      readOnly={readOnly}
                      items={options.groupTypes.map((value) => ({
                        value,
                        label: value,
                      }))}
                      value={current.type}
                      onValueChange={(value) => patch({ type: String(value) })}
                    >
                      <SelectTrigger id="egress-type" className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {options.groupTypes.map((value) => (
                            <SelectItem key={value} value={value}>
                              {value}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                  {current.type !== "select" && (
                    <Field>
                      <FieldLabel htmlFor="egress-interval">
                        测速间隔 (秒)
                      </FieldLabel>
                      <NumberInput
                        id="egress-interval"
                        readOnly={readOnly}
                        min={0}
                        integer
                        value={current.interval}
                        onValueChange={(interval) => patch({ interval })}
                      />
                    </Field>
                  )}
                </FieldGroup>
                {current.type !== "select" && (
                  <FieldGroup className="grid gap-4 sm:grid-cols-2">
                    <Field>
                      <FieldLabel htmlFor="egress-test-url">测速地址</FieldLabel>
                      <Input
                        id="egress-test-url"
                        readOnly={readOnly}
                        placeholder="留空用 gstatic generate_204"
                        value={current.testUrl}
                        onChange={(event) =>
                          patch({ testUrl: event.target.value })
                        }
                      />
                    </Field>
                    {current.type === "url-test" && (
                      <Field>
                        <FieldLabel htmlFor="egress-tolerance">
                          切换容差 (毫秒)
                        </FieldLabel>
                        <NumberInput
                          id="egress-tolerance"
                          readOnly={readOnly}
                          min={0}
                          integer
                          value={current.tolerance}
                          onValueChange={(tolerance) => patch({ tolerance })}
                        />
                        <FieldDescription>
                          新节点要比当前快这么多才切换。
                        </FieldDescription>
                      </Field>
                    )}
                  </FieldGroup>
                )}

                <FieldSet>
                  <FieldLegend variant="label">成员</FieldLegend>
                  <FieldDescription>
                    顺序就是客户端里的排列顺序，支持拖拽或左右按钮调整；解析不到的成员会被自动跳过。
                  </FieldDescription>
                  <div className="flex flex-wrap items-center gap-2">
                    {current.members.map((member, index) => (
                      <Badge
                        key={`${member.kind}:${member.ref}:${index}`}
                        variant={memberMissing(member) ? "destructive" : "secondary"}
                        className={cn(
                          readOnly ? undefined : "pl-1.5 pr-0.5",
                          dragOverMemberIndex === index &&
                            dragMemberIndex !== null &&
                            dragMemberIndex !== index &&
                            "ring-2 ring-primary",
                        )}
                        draggable={!readOnly}
                        onDragStart={(event) => {
                          if (readOnly) return;
                          setDragMemberIndex(index);
                          event.dataTransfer.effectAllowed = "move";
                          event.dataTransfer.setData("text/plain", String(index));
                        }}
                        onDragOver={(event) => {
                          if (readOnly || dragMemberIndex === null) return;
                          event.preventDefault();
                          event.dataTransfer.dropEffect = "move";
                          if (dragOverMemberIndex !== index)
                            setDragOverMemberIndex(index);
                        }}
                        onDrop={(event) => {
                          if (readOnly || dragMemberIndex === null) return;
                          event.preventDefault();
                          if (dragMemberIndex !== index) {
                            patch({
                              members: reorder(current.members, dragMemberIndex, index),
                            });
                          }
                          setDragMemberIndex(null);
                          setDragOverMemberIndex(null);
                        }}
                        onDragEnd={() => {
                          setDragMemberIndex(null);
                          setDragOverMemberIndex(null);
                        }}
                        title={
                          memberMissing(member)
                            ? "找不到这个成员，渲染时会跳过"
                            : undefined
                        }
                      >
                        {!readOnly && (
                          <GripVerticalIcon className="size-3 shrink-0 cursor-grab text-muted-foreground" />
                        )}
                        {memberMissing(member) && (
                          <TriangleAlertIcon data-icon="inline-start" />
                        )}
                        {memberLabel(member)}
                        {!readOnly && (
                          <>
                            <Button
                              variant="ghost"
                              size="icon-xs"
                              className="size-4"
                              aria-label={`把成员 ${index + 1} 前移`}
                              disabled={index === 0}
                              onClick={() =>
                                patch({ members: move(current.members, index, -1) })
                              }
                            >
                              <ArrowLeftIcon />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-xs"
                              className="size-4"
                              aria-label={`把成员 ${index + 1} 后移`}
                              disabled={index === current.members.length - 1}
                              onClick={() =>
                                patch({ members: move(current.members, index, 1) })
                              }
                            >
                              <ArrowRightIcon />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-xs"
                              className="size-4"
                              aria-label={`移除成员 ${index + 1}`}
                              onClick={() =>
                                patch({
                                  members: current.members.filter(
                                    (_, i) => i !== index,
                                  ),
                                })
                              }
                            >
                              <XIcon />
                            </Button>
                          </>
                        )}
                      </Badge>
                    ))}
                    {!readOnly && (
                      <MemberPicker
                        groups={groups}
                        policies={policies.filter((p) => p.name !== policyName)}
                        builtins={options.builtins}
                        existingMembers={current.members}
                        onPick={(member) =>
                          patch({ members: [...current.members, member] })
                        }
                      />
                    )}
                  </div>
                  {current.members.length === 0 && (
                    <FieldDescription>
                      成员为空的策略组会被渲染时丢弃，指向它的规则会落到兜底。
                    </FieldDescription>
                  )}
                </FieldSet>
              </>
            )}

            {mode === "inherit" && inherited && (
              <FieldDescription>
                当前跟随默认：{inherited.type} ·{" "}
                {inherited.members.map(memberLabel).join(" · ") || "（空）"}
              </FieldDescription>
            )}
          </FieldGroup>
        </DialogBody>

        <DialogFooter>
          {readOnly ? (
            <Button onClick={onClose}>关闭</Button>
          ) : (
            <>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                取消
              </Button>
              <Button onClick={onConfirm}>确认</Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function MemberPicker({
  groups,
  policies,
  builtins,
  existingMembers,
  onPick,
}: {
  groups: NodeGroup[];
  policies: Policy[];
  builtins: string[];
  existingMembers: EgressMember[];
  onPick: (member: EgressMember) => void;
}) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  const hasMember = (kind: MemberKind, ref: string) =>
    existingMembers.some((member) => member.kind === kind && member.ref === ref);

  const pick = (kind: MemberKind, ref: string) => {
    if (hasMember(kind, ref)) return;
    onPick({ kind, ref });
    setOpen(false);
    setSearch("");
  };

  const keyword = search.trim().toLocaleLowerCase("zh-CN");
  const matches = (label: string) =>
    !keyword || label.toLocaleLowerCase("zh-CN").includes(keyword);

  const dynamicCandidates: { kind: MemberKind; ref: string; label: string }[] = [
    { kind: "all-node-groups", ref: "", label: "全部节点组（按方案收窄）" },
    { kind: "all-inbounds", ref: "", label: "全部入站" },
  ];
  const dynamicItems = dynamicCandidates.filter((item) => matches(item.label));

  const filteredGroups = groups.filter((group) =>
    matches(group.emoji ? `${group.emoji} ${group.name}` : group.name),
  );
  const filteredPolicies = policies.filter((policy) =>
    matches(policy.icon ? `${policy.icon} ${policy.name}` : policy.name),
  );
  const filteredBuiltins = builtins
    .map((builtin) => ({
      builtin,
      label: builtin === "DIRECT-V6" ? "DIRECT-V6（直连，IPv6 优先）" : builtin,
    }))
    .filter((item) => matches(item.label));

  const hasAnyResult =
    dynamicItems.length > 0 ||
    filteredGroups.length > 0 ||
    filteredPolicies.length > 0 ||
    filteredBuiltins.length > 0;

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setSearch("");
      }}
    >
      <PopoverTrigger render={<Button type="button" variant="outline" size="sm" />}>
        <PlusIcon data-icon="inline-start" />
        添加成员
        <ChevronDownIcon data-icon="inline-end" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-72 p-0">
        <div className="border-b p-2">
          <SearchInput
            value={search}
            onChange={setSearch}
            placeholder="搜索节点组、策略或内置…"
            ariaLabel="搜索出口成员"
            clearLabel="清除搜索"
            className="sm:w-full"
          />
        </div>
        <div className="max-h-72 overflow-y-auto p-1">
          {!hasAnyResult ? (
            <p className="px-2 py-4 text-center text-xs text-muted-foreground">
              没有匹配的成员
            </p>
          ) : (
            <div className="flex flex-col gap-1">
              {dynamicItems.length > 0 && (
                <div>
                  <div className="px-2 py-1 text-xs font-medium text-muted-foreground">
                    动态展开
                  </div>
                  {dynamicItems.map((item) => {
                    const added = hasMember(item.kind, item.ref);
                    return (
                      <button
                        key={item.kind}
                        type="button"
                        disabled={added}
                        onClick={() => pick(item.kind, item.ref)}
                        className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50"
                      >
                        <span className="truncate">{item.label}</span>
                        {added && (
                          <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                            已添加
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}

              {filteredGroups.length > 0 && (
                <div className="border-t pt-1">
                  <div className="px-2 py-1 text-xs font-medium text-muted-foreground">
                    节点组
                  </div>
                  {filteredGroups.map((group) => {
                    const added = hasMember("node-group", group.name);
                    const label = group.emoji ? `${group.emoji} ${group.name}` : group.name;
                    return (
                      <button
                        key={group.id}
                        type="button"
                        disabled={added}
                        onClick={() => pick("node-group", group.name)}
                        className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50"
                      >
                        <span className="truncate">{label}</span>
                        {added && (
                          <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                            已添加
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}

              {filteredPolicies.length > 0 && (
                <div className="border-t pt-1">
                  <div className="px-2 py-1 text-xs font-medium text-muted-foreground">
                    分流策略
                  </div>
                  {filteredPolicies.map((policy) => {
                    const added = hasMember("policy", policy.name);
                    const label = policy.icon ? `${policy.icon} ${policy.name}` : policy.name;
                    return (
                      <button
                        key={policy.name}
                        type="button"
                        disabled={added}
                        onClick={() => pick("policy", policy.name)}
                        className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50"
                      >
                        <span className="truncate">{label}</span>
                        {added && (
                          <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                            已添加
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}

              {filteredBuiltins.length > 0 && (
                <div className="border-t pt-1">
                  <div className="px-2 py-1 text-xs font-medium text-muted-foreground">
                    内置策略
                  </div>
                  {filteredBuiltins.map((item) => {
                    const added = hasMember("builtin", item.builtin);
                    return (
                      <button
                        key={item.builtin}
                        type="button"
                        disabled={added}
                        onClick={() => pick("builtin", item.builtin)}
                        className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-sm transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-50"
                      >
                        <span className="truncate">{item.label}</span>
                        {added && (
                          <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                            已添加
                          </span>
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function ProfileDialog({
  initialDraft,
  draft,
  saving,
  groups,
  onChange,
  onClose,
  onSave,
}: {
  initialDraft: ProfileDraft | null;
  draft: ProfileDraft | null;
  saving: boolean;
  groups: NodeGroup[];
  onChange: (draft: ProfileDraft) => void;
  onClose: () => void;
  onSave: () => void;
}) {
  const [attempted, setAttempted] = useState(false);
  const open = draft !== null;
  useEffect(() => {
    if (!open) setAttempted(false);
  }, [open]);
  const dirty = Boolean(
    draft && initialDraft && JSON.stringify(draft) !== JSON.stringify(initialDraft),
  );
  const onOpenChange = useDirtyDialog(dirty, saving, onClose);
  const nameInvalid = attempted && !draft?.name.trim();

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex flex-col overflow-hidden sm:max-w-xl" showCloseButton={!saving}>
        <form
          className="contents"
          noValidate
          aria-busy={saving}
          onSubmit={(event) => {
            event.preventDefault();
            setAttempted(true);
            if (!saving && draft?.name.trim()) onSave();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {draft?.id ? "编辑分流方案" : "新建分流方案"}
            </DialogTitle>
            <DialogDescription>
              可用节点组决定这个方案的用户能连上哪些入站。保存后会立刻同步到相关面板。
            </DialogDescription>
          </DialogHeader>
          <DialogBody>
            {draft && (
              <FieldSet disabled={saving}>
                <FieldGroup>
                  <Field data-invalid={nameInvalid || undefined}>
                    <FieldLabel htmlFor="profile-name">名称</FieldLabel>
                    <Input
                      id="profile-name"
                      required
                      placeholder="VIP"
                      value={draft.name}
                      aria-invalid={nameInvalid || undefined}
                      onChange={(event) =>
                        onChange({ ...draft, name: event.target.value })
                      }
                    />
                    {nameInvalid && <FieldError>请输入方案名称。</FieldError>}
                  </Field>

                  <Field orientation="horizontal">
                    <FieldLabel htmlFor="profile-all">包含全部节点组</FieldLabel>
                    <Switch
                      id="profile-all"
                      checked={draft.allGroups}
                      onCheckedChange={(allGroups) =>
                        onChange({ ...draft, allGroups })
                      }
                    />
                  </Field>

                  {!draft.allGroups && (
                    <FieldSet>
                      <FieldLegend variant="label">
                        可用节点组（已选 {draft.groupIds.length} 个）
                      </FieldLegend>
                      <ScrollArea className="h-56 rounded-lg border">
                        <FieldGroup className="gap-2 p-3">
                          {groups.length === 0 ? (
                            <FieldDescription>
                              还没有节点组，先去「节点组」页建一个。
                            </FieldDescription>
                          ) : (
                            groups.map((group) => (
                              <Field key={group.id} orientation="horizontal">
                                <Checkbox
                                  id={`profile-group-${group.id}`}
                                  checked={draft.groupIds.includes(group.id)}
                                  onCheckedChange={() =>
                                    onChange({
                                      ...draft,
                                      groupIds: draft.groupIds.includes(group.id)
                                        ? draft.groupIds.filter(
                                            (id) => id !== group.id,
                                          )
                                        : [...draft.groupIds, group.id],
                                    })
                                  }
                                />
                                <FieldLabel htmlFor={`profile-group-${group.id}`}>
                                  <span>
                                    {group.emoji
                                      ? `${group.emoji} ${group.name}`
                                      : group.name}
                                  </span>
                                  <span className="text-xs text-muted-foreground">
                                    {group.usableInbounds} 个入站
                                  </span>
                                  {!group.enabled && (
                                    <StatusBadge tone="idle">停用</StatusBadge>
                                  )}
                                </FieldLabel>
                              </Field>
                            ))
                          )}
                        </FieldGroup>
                      </ScrollArea>
                    </FieldSet>
                  )}

                  <Field orientation="horizontal">
                    <FieldLabel htmlFor="profile-enabled">启用</FieldLabel>
                    <Switch
                      id="profile-enabled"
                      checked={draft.enabled}
                      onCheckedChange={(enabled) => onChange({ ...draft, enabled })}
                    />
                  </Field>

                  <Field>
                    <FieldLabel htmlFor="profile-remark">备注</FieldLabel>
                    <Input
                      id="profile-remark"
                      value={draft.remark}
                      onChange={(event) =>
                        onChange({ ...draft, remark: event.target.value })
                      }
                    />
                  </Field>
                </FieldGroup>
              </FieldSet>
            )}
          </DialogBody>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={saving}
              onClick={() => onOpenChange(false)}
            >
              取消
            </Button>
            <Button type="submit" disabled={saving}>
              {saving && <Spinner data-icon="inline-start" />}
              {saving ? "保存中…" : "保存方案"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
