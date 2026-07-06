import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { readFile, stat } from "node:fs/promises";
import path from "node:path";
import { Readable } from "node:stream";
import type { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import { z } from "zod";
import type { ArtifactKind } from "./domain/artifacts.js";
import type { CoordinatorStorage } from "./storage/index.js";
import { StorageError, StorageObjectTooLargeError } from "./storage/index.js";
import type { ArtifactManifest } from "./storage/index.js";
import type { WorldSnapshotManifest } from "./storage/index.js";
import type { ArtifactMetadata, InMemoryCoordinatorStore, GcBackend } from "./store.js";
import { GcBlockedError, StoreError } from "./store.js";
import type { TunnelSession, PlayerSession } from "./network.js";
import type { RelayManager } from "./relay.js";

const jsonObjectUploadDecodedLimitBytes = 16 * 1024 * 1024;
const jsonObjectUploadBodyLimitBytes = 24 * 1024 * 1024;
const manifestUploadBodyLimitBytes = 1024 * 1024;
const inviteJoinRateLimits = new Map<string, { count: number; resetAt: number }>();
export const defaultMaxObjectBytes = 256 * 1024 * 1024;

const bootstrapPackageDefinitions = [
  {
    id: "acbh-runtime-base-windows-amd64",
    version: "0.3.3",
    filename: "acbh-runtime-base-windows-amd64.zip",
    requiredFor: ["desktop-bootstrap"],
  },
  {
    id: "java-17-windows-amd64",
    version: "17.x",
    filename: "acbh-java-17-windows-amd64.zip",
    requiredFor: ["minecraft-1.18+"],
  },
  {
    id: "java-21-windows-amd64",
    version: "21.x",
    filename: "acbh-java-21-windows-amd64.zip",
    requiredFor: ["minecraft-1.20.5+"],
  },
] as const;

const createGroupSchema = z.object({
  name: z.string().trim().min(1).max(120),
  ownerName: z.string().trim().min(1).max(80),
});

const joinGroupParamsSchema = z.object({
  groupId: z.string().min(1),
});

const bootstrapPackageParamsSchema = z.object({
  filename: z.string().regex(/^[A-Za-z0-9._-]+\.zip$/),
});

const joinGroupSchema = z.object({
  accessKey: z.string().min(1),
  displayName: z.string().trim().min(1).max(80),
});

const createInviteSchema = z.object({
  accessKey: z.string().min(1).optional(),
  hostId: z.string().min(1).optional(),
  hostToken: z.string().min(1).optional(),
  expiresInSeconds: z.number().int().positive().optional(),
  oneTime: z.boolean().optional(),
});

const revokeInviteSchema = z.object({
  accessKey: z.string().min(1).optional(),
  hostId: z.string().min(1).optional(),
  hostToken: z.string().min(1).optional(),
  inviteId: z.string().min(1),
});

const listInviteSchema = z.object({
  accessKey: z.string().min(1).optional(),
  hostId: z.string().min(1).optional(),
  hostToken: z.string().min(1).optional(),
});

const joinInviteSchema = z.object({
  inviteCode: z.string().trim().min(1).max(80),
  displayName: z.string().trim().min(1).max(80),
  deviceName: z.string().trim().min(1).max(120),
  platform: z.string().trim().min(1).max(40),
  agentVersion: z.string().trim().min(1).max(40),
});

const registerHostSchema = z.object({
  groupId: z.string().min(1),
  accessKey: z.string().min(1).optional(),
  memberId: z.string().min(1),
  deviceName: z.string().trim().min(1).max(120),
  platform: z.string().trim().min(1).max(40),
  agentVersion: z.string().trim().min(1).max(40),
});

const heartbeatSchema = z.object({
  groupId: z.string().min(1),
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
  status: z.enum(["online", "standby", "hosting", "unhealthy", "offline"]),
  latestLocalSnapshotId: z.string().min(1).nullable().optional(),
  latestLocalArtifacts: z
    .object({
      "world-snapshot": z.string().min(1).optional(),
      "server-pack": z.string().min(1).optional(),
      "admin-state": z.string().min(1).optional(),
    })
    .optional(),
  hostScoreHints: z
    .object({
      cpuCores: z.number().int().nonnegative().optional(),
      memoryTotalBytes: z.number().int().nonnegative().optional(),
      diskFreeBytes: z.number().int().nonnegative().optional(),
      javaAvailable: z.boolean().optional(),
    })
    .optional(),
  connection: z
    .object({
      host: z.string().trim().min(1).max(255),
      port: z.number().int().min(1).max(65535),
      network: z.string().trim().min(1).max(40),
    })
    .nullable()
    .optional(),
});

const groupStateParamsSchema = z.object({
  groupId: z.string().min(1),
});

const electionParamsSchema = z.object({
  groupId: z.string().min(1),
});

const electionAuthSchema = z.object({
  groupId: z.string().min(1),
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
});

const electionRunSchema = electionAuthSchema.extend({
  reason: z.enum(["manual", "heartbeat-timeout", "no-current-host"]),
});

const takeoverActionSchema = electionAuthSchema.extend({
  assignmentId: z.string().min(1),
  takeoverToken: z.string().min(1),
});

const takeoverPollSchema = electionAuthSchema.extend({
  dryRun: z.boolean().optional(),
});

const takeoverFailSchema = takeoverActionSchema.extend({
  failureReason: z.string().trim().min(1).max(200),
});

const artifactKinds = ["server-pack", "world-snapshot", "admin-state"] as const;

const uploadObjectSchema = z.object({
  groupId: z.string().min(1),
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
  sha256: z.string().regex(/^[a-f0-9]{64}$/),
  contentBase64: z.string(),
});

const uploadManifestSchema = z.object({
  groupId: z.string().min(1),
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
  artifactKind: z.enum(artifactKinds),
  artifactId: z.string().min(1),
  manifest: z.object({}).passthrough(),
});

const groupArtifactsParamsSchema = z.object({
  groupId: z.string().min(1),
});

const latestArtifactQuerySchema = z.object({
  artifactKind: z.enum(artifactKinds),
});

const artifactManifestParamsSchema = z.object({
  groupId: z.string().min(1),
  artifactKind: z.enum(artifactKinds),
  artifactId: z.string().min(1),
});

const playerSessionParamsSchema = z.object({
  groupId: z.string().min(1),
});

const playerSessionCreateSchema = z.object({
  displayName: z.string().trim().min(1).max(80).optional(),
});

const tunnelSessionParamsSchema = z.object({
  groupId: z.string().min(1),
});

const tunnelSessionGetParamsSchema = z.object({
  groupId: z.string().min(1),
  sessionId: z.string().min(1),
});

const tunnelSessionListParamsSchema = z.object({
  groupId: z.string().min(1),
});

const tunnelSessionCreateSchema = z.object({
  playerId: z.string().min(1),
});

const objectParamsSchema = z.object({
  groupId: z.string().min(1),
  sha256: z.string().regex(/^[a-f0-9]{64}$/),
});

const worldGroupParamsSchema = z.object({
  groupId: z.string().min(1),
});

const worldSnapshotParamsSchema = z.object({
  groupId: z.string().min(1),
  snapshotId: z.string().min(1),
});

const worldLatestQuerySchema = z.object({
  consistentOnly: z.coerce.boolean().optional().default(false),
});

const worldObjectParamsSchema = z.object({
  groupId: z.string().min(1),
  sha256: z.string().regex(/^[a-f0-9]{64}$/),
});

const worldBackupPlanSchema = z.object({
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
  hostGeneration: z.number().int().nonnegative(),
  parentSnapshotId: z.string().optional(),
  objects: z.array(z.object({
    sha256: z.string().regex(/^[a-f0-9]{64}$/),
    size: z.number().int().nonnegative(),
    path: z.string().optional(),
  })).max(100_000),
});

const worldSnapshotFileSchema = z.object({
  path: z.string().min(1).max(4096),
  size: z.number().int().nonnegative(),
  sha256: z.string().regex(/^[a-f0-9]{64}$/),
  objectId: z.string().regex(/^sha256:[a-f0-9]{64}$/),
});

const worldSnapshotManifestSchema = z.object({
  schemaVersion: z.literal(1),
  snapshotId: z.string().min(1).max(128),
  groupId: z.string().min(1).max(128),
  sourceHostId: z.string().min(1).max(128),
  hostGeneration: z.number().int().nonnegative(),
  parentSnapshotId: z.string().optional(),
  createdAt: z.string().datetime(),
  consistent: z.boolean(),
  logicalSize: z.number().int().nonnegative(),
  uploadedSize: z.number().int().nonnegative(),
  fileCount: z.number().int().nonnegative(),
  changedFileCount: z.number().int().nonnegative(),
  deletedFileCount: z.number().int().nonnegative(),
  files: z.array(worldSnapshotFileSchema).max(250_000),
  deletedPaths: z.array(z.string().min(1).max(4096)).optional(),
});

const worldBackupCommitSchema = z.object({
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
  hostGeneration: z.number().int().nonnegative(),
  manifest: worldSnapshotManifestSchema,
});

const worldBackupPinSchema = z.object({
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
  pinned: z.boolean().optional().default(true),
});

const streamingObjectParamsSchema = z.object({
  sha256: z.string().regex(/^[a-f0-9]{64}$/),
});

const hostAuthHeaderSchema = z.object({
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
});

const streamingUploadHeaderSchema = z.object({
  groupId: z.string().min(1),
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
});

const leaseEnsureSchema = z.object({
  groupId: z.string().min(1),
  hostId: z.string().min(1),
  hostToken: z.string().min(1),
  generation: z.number().int().nonnegative().optional(),
});

const bootstrapIdentitySchema = z.object({
  instanceId: z.string().min(1),
  instanceName: z.string().trim().min(1).max(120),
  deviceId: z.string().min(1),
  deviceName: z.string().trim().min(1).max(120),
  serverId: z.string().min(1),
  serverName: z.string().trim().min(1).max(120),
});

const coordinatorVersion = process.env.ACBH_VERSION ?? "v0.5.1-public-relay-hotfix";
const buildCommit = process.env.ACBH_BUILD_COMMIT ?? "dev";
const protocolVersion = Number.parseInt(process.env.ACBH_PROTOCOL_VERSION ?? "2", 10);
const minimumClientProtocol = 2;
const alpha6Capabilities = [
  "capabilities_v1",
  "desktop_protocol_v2",
  "world_backup_resume",
  "public_relay_v1",
  "bootstrap_packages_v1",
  "invite_management_v1",
  "lease_renew_v1",
  "world_backup_v1",
  "group_whoami_v1",
  "token_only_relay_v1",
  "bootstrap_upsert_v1",
] as const;

export async function registerRoutes(
  app: FastifyInstance,
  store: InMemoryCoordinatorStore,
  storage: CoordinatorStorage,
  relay: RelayManager,
  options?: { maxObjectBytes?: number },
): Promise<void> {
  const maxObjectBytes = resolveMaxObjectBytes(options?.maxObjectBytes);
  app.get("/health", async () => {
    return {
      ok: true,
      service: "acbh-coordinator",
      version: coordinatorVersion,
      coordinatorVersion,
      buildCommit,
      protocolVersion,
      minimumClientProtocol,
    };
  });

  app.get("/version", async () => {
    return {
      ok: true,
      service: "acbh-coordinator",
      version: coordinatorVersion,
      coordinatorVersion,
      buildCommit,
      protocolVersion,
      minimumClientProtocol,
      capabilities: [...alpha6Capabilities],
    };
  });

  app.get("/v1/capabilities", async () => {
    return {
      coordinatorVersion,
      buildCommit,
      protocolVersion,
      minimumClientProtocol,
      capabilities: [...alpha6Capabilities],
      serverTime: new Date().toISOString(),
      authenticationMode: "access_token_bearer",
    };
  });

  app.post("/v1/bootstrap", async (request, reply) => {
    const accessToken = parseBearerToken(request);
    if (!accessToken) {
      return reply.code(401).send({
        error: "Unauthorized",
        code: "access_token_required",
        message: "Authorization Bearer access token is required",
      });
    }
    const body = parseBody(bootstrapIdentitySchema, request, reply);
    if (!body) {
      return reply;
    }
    return handleStoreCall(reply, () => store.bootstrap({ ...body, accessToken }));
  });

  app.post("/v1/auth/verify", async (request, reply) => {
    const accessToken = parseBearerToken(request);
    if (!accessToken) {
      return reply.code(401).send({
        error: "Unauthorized",
        code: "access_token_required",
        message: "Authorization Bearer access token is required",
      });
    }
    return handleStoreCall(reply, () => store.verifyAccessToken(accessToken));
  });

  app.get("/v1/info", async () => {
    return {
      name: "ACBH Coordinator",
      mode: "in-memory",
      v1NonGoal: "hot-migration",
      persistence: "none",
    };
  });

  app.get("/v1/bootstrap/manifest", async (request) => {
    const origin = `${request.protocol}://${request.hostname}`;
    const packages = await Promise.all(bootstrapPackageDefinitions.map((definition) => describeBootstrapPackage(definition, origin)));
    return {
      version: 1,
      packages,
    };
  });

  app.get("/v1/bootstrap/packages/:filename", async (request, reply) => {
    const params = parseParams(bootstrapPackageParamsSchema, request, reply);
    if (!params) {
      return reply;
    }
    const definition = bootstrapPackageDefinitions.find((p) => p.filename === params.filename);
    if (!definition) {
      return reply.code(404).send({
        error: "Not Found",
        message: "Bootstrap package does not exist",
      });
    }
    const filePath = path.join(bootstrapPackageDir(), definition.filename);
    try {
      const info = await stat(filePath);
      if (!info.isFile()) {
        throw new Error("not a file");
      }
    } catch {
      return reply.code(404).send({
        error: "Not Found",
        message: "Bootstrap package is not installed on this Coordinator",
      });
    }
    return reply.header("content-type", "application/zip").send(createReadStream(filePath));
  });

  app.get("/v1/storage/info", async () => {
    return storage.info();
  });

  app.post(
    "/v1/artifacts/objects",
    { bodyLimit: jsonObjectUploadBodyLimitBytes },
    async (request, reply) => {
      const body = parseBody(uploadObjectSchema, request, reply);
      if (!body) {
        return reply;
      }

      return handleStoreCall(reply, () => {
        store.verifyHost(body);
        const content = decodeBase64(body.contentBase64, reply);
        if (!content) {
          return reply;
        }
        if (content.length > jsonObjectUploadDecodedLimitBytes) {
          return reply.code(413).send({
            error: "Payload Too Large",
            message: "Object upload exceeds 16 MiB JSON/base64 limit",
          });
        }

        const actual = sha256(content);
        if (actual !== body.sha256) {
          return reply.code(400).send({
            error: "Bad Request",
            message: "Object content does not match declared sha256",
          });
        }

        return handleStorageCall(reply, async () => {
          const exists = await storage.objectExists({
            groupId: body.groupId,
            sha256: body.sha256,
          });
          if (!exists) {
            await storage.saveObject({
              groupId: body.groupId,
              sha256: body.sha256,
              content,
            });
          }

          return {
            ok: true,
            sha256: body.sha256,
            exists,
          };
        });
      });
    },
  );

  app.put("/v1/artifacts/objects/:sha256", async (request, reply) => {
    const params = parseParams(streamingObjectParamsSchema, request, reply);
    const authResult = streamingUploadHeaderSchema.safeParse({
      groupId: request.headers["x-acbh-group-id"],
      hostId: request.headers["x-acbh-host-id"],
      hostToken: request.headers["x-acbh-host-token"],
    });
    if (!params) {
      return reply;
    }
    if (!authResult.success) {
      return reply.code(401).send({
        error: "Unauthorized",
        message: "Streaming upload authentication headers are required",
        issues: authResult.error.issues,
      });
    }
    const bodyStream = request.body;
    if (!(bodyStream instanceof Readable)) {
      return reply.code(400).send({
        error: "Bad Request",
        message: "Request body must be application/octet-stream",
      });
    }

    const declaredLength = parseContentLength(request.headers["content-length"]);
    if (declaredLength !== null && declaredLength > maxObjectBytes) {
      bodyStream.resume();
      return reply.code(413).send({
        error: "Payload Too Large",
        message: `Object upload exceeds configured limit of ${maxObjectBytes} bytes`,
      });
    }

    return handleStoreCall(reply, () => {
      store.verifyHost(authResult.data);

      return handleStorageCall(reply, async () => {
        const exists = await storage.objectExists({
          groupId: authResult.data.groupId,
          sha256: params.sha256,
        });
        if (exists) {
          bodyStream.resume();
          const existing = await storage.createObjectReadStream({
            groupId: authResult.data.groupId,
            sha256: params.sha256,
          });
          existing.stream.destroy();
          return {
            ok: true,
            sha256: params.sha256,
            exists: true,
            size: existing.size,
          };
        }

        const saved = await storage.saveObjectFromStream({
          groupId: authResult.data.groupId,
          sha256: params.sha256,
          stream: bodyStream,
          maxBytes: maxObjectBytes,
        });

        return {
          ok: true,
          sha256: params.sha256,
          exists: false,
          size: saved.size,
        };
      });
    });
  });

  app.post("/v1/artifacts/manifests", { bodyLimit: manifestUploadBodyLimitBytes }, async (request, reply) => {
    const body = parseBody(uploadManifestSchema, request, reply);
    if (!body) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      const uploadedManifest = body.manifest as unknown as ArtifactManifest;
      const hostGeneration = parseOptionalIntHeader(request.headers["x-acbh-host-generation"]);
      const publishAuth = {
        metadata: {
          groupId: body.groupId,
          creatorHostId: uploadedManifest.creatorHostId,
        },
        hostId: body.hostId,
        hostToken: body.hostToken,
        currentHostGeneration: hostGeneration ?? undefined,
      };

      store.authorizeArtifactPublish(publishAuth);

      if (
        uploadedManifest.groupId !== body.groupId ||
        uploadedManifest.artifactKind !== body.artifactKind ||
        uploadedManifest.artifactId !== body.artifactId
      ) {
        return reply.code(400).send({
          error: "Bad Request",
          message: "Manifest IDs must match upload request",
        });
      }
      const metadata = {
        groupId: body.groupId,
        artifactKind: body.artifactKind,
        artifactId: body.artifactId,
        parentArtifactId: uploadedManifest.parentArtifactId ?? null,
        serverPackVersion:
          uploadedManifest.serverPackVersion ?? (body.artifactKind === "server-pack" ? body.artifactId : null),
        creatorHostId: uploadedManifest.creatorHostId,
        createdAt: uploadedManifest.createdAt,
        status: "available" as const,
        manifestSha256: sha256(Buffer.from(JSON.stringify(uploadedManifest), "utf8")),
        manifestObjectPath: manifestStorageKey(body.groupId, body.artifactKind, body.artifactId),
        fileCount: countIncludedFiles(uploadedManifest),
        totalBytes: totalManifestBytes(uploadedManifest),
      };

      return handleStorageCall(reply, async () => {
        for (const file of uploadedManifest.files ?? []) {
          if (file.deleted) {
            continue;
          }
          const exists = await storage.objectExists({
            groupId: body.groupId,
            sha256: file.sha256,
          });
          if (!exists) {
            return reply.code(400).send({
              error: "Bad Request",
              message: `Missing object ${file.sha256} for ${file.path}`,
            });
          }
        }

        await storage.saveManifest({
          groupId: body.groupId,
          artifactKind: body.artifactKind,
          artifactId: body.artifactId,
          manifest: uploadedManifest,
          beforeCommit: () => store.authorizeArtifactPublish(publishAuth),
        });

        const recorded = store.recordArtifact(metadata);

        return {
          ok: true,
          artifactKind: recorded.artifactKind,
          artifactId: recorded.artifactId,
          status: recorded.status,
        };
      });
    });
  });

  const gcBodySchema = z.object({
    dryRun: z.boolean().optional().default(true),
    retentionPerKind: z.number().int().positive().optional(),
    minAgeMs: z.number().int().positive().optional(),
  });

  app.post("/v1/groups/:groupId/artifacts/gc", async (request, reply) => {
    const params = parseParams(
      z.object({ groupId: z.string().min(1) }),
      request,
      reply,
    );
    if (!params) return reply;

    const hostId = singleHeader(request.headers["x-acbh-host-id"]);
    const hostToken = singleHeader(request.headers["x-acbh-host-token"]);
    const hostGeneration = parseOptionalIntHeader(request.headers["x-acbh-host-generation"]);

    if (!hostId || !hostToken) {
      reply.code(401).send({
        error: "Unauthorized",
        message: "Host authentication headers are required (X-ACBH-Host-ID, X-ACBH-Host-Token)",
      });
      return reply;
    }

    const body = parseBody(gcBodySchema, request, reply);
    if (!body) return reply;

    const gcBackend: GcBackend = {
      deleteManifest: async (p) => {
        await storage.deleteManifest({ groupId: p.groupId, artifactKind: p.artifactKind, artifactId: p.artifactId });
      },
      deleteObject: async (p) => {
        await storage.deleteObject({ groupId: p.groupId, sha256: p.sha256 });
      },
      listObjectSha256s: async (p) => {
        return storage.listObjectSha256s({ groupId: p.groupId });
      },
      readManifestFiles: async (p) => {
        const manifest = await storage.readManifest({
          groupId: p.groupId,
          artifactKind: p.artifactKind,
          artifactId: p.artifactId,
        });
        return (manifest.files ?? []).map((f) => ({ sha256: f.sha256, deleted: !!f.deleted }));
      },
    };

    try {
      const result = await store.gcArtifacts({
        groupId: params.groupId,
        dryRun: body.dryRun,
        hostId,
        hostToken,
        currentHostGeneration: hostGeneration ?? undefined,
        retentionPerKind: body.retentionPerKind,
        minAgeMs: body.minAgeMs,
        backend: gcBackend,
      });
      return result;
    } catch (error) {
      if (error instanceof GcBlockedError) {
        reply.code(error.statusCode).send({
          error: statusText(error.statusCode),
          message: error.message,
          blocked: true,
          blockers: error.blockers,
        });
        return reply;
      }
      if (error instanceof StoreError) {
        reply.code(error.statusCode).send({
          error: statusText(error.statusCode),
          message: error.message,
          ...(error.code ? { code: error.code } : {}),
        });
        return reply;
      }
      throw error;
    }
  });

  app.post("/v1/groups/:groupId/tunnel-sessions", async (request, reply) => {
    const params = parseParams(tunnelSessionParamsSchema, request, reply);
    const body = parseBody(tunnelSessionCreateSchema, request, reply);
    if (!params || !body) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      if (!verifyRequestPlayer(store, params.groupId, body.playerId, request, reply)) {
        return reply;
      }
      const session = store.createTunnelSession({
        groupId: params.groupId,
        playerId: body.playerId,
      });
      return { ...session };
    });
  });

  app.get("/v1/groups/:groupId/tunnel-sessions/:sessionId", async (request, reply) => {
    const params = parseParams(tunnelSessionGetParamsSchema, request, reply);
    if (!params) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      const session = store.getTunnelSession(params.groupId, params.sessionId);
      if (!verifyRequestPlayer(store, params.groupId, session.playerId, request, reply)) {
        return reply;
      }
      return { ...session };
    });
  });

  app.get("/v1/groups/:groupId/tunnel-sessions", async (request, reply) => {
    const params = parseParams(tunnelSessionListParamsSchema, request, reply);
    if (!params) {
      return reply;
    }

    // list may require host or admin auth in future; for now allow for relay host manager polling
    return handleStoreCall(reply, () => store.listTunnelSessions(params.groupId));
  });

  app.get(
    "/v1/groups/:groupId/relay/clients/host",
    { websocket: true },
    (socket, request) => {
      const params = parseWSParams(groupStateParamsSchema, request, (code, reason) => {
        try { socket.socket.close(code, reason); } catch { socket.destroy(); }
      });
      if (!params) return;

      const hostId = singleHeader(request.headers["x-acbh-host-id"]);
      const hostToken = singleHeader(request.headers["x-acbh-host-token"]);
      const hostGeneration = parseOptionalIntHeader(request.headers["x-acbh-host-generation"]);

      if (!hostId || !hostToken) {
        socket.socket.close(4001, "Host authentication headers are required (X-ACBH-Host-ID, X-ACBH-Host-Token)");
        return;
      }
      if (hostGeneration === null || hostGeneration === undefined) {
        socket.socket.close(4001, "Host generation header is required (X-ACBH-Host-Generation)");
        return;
      }

      try {
        store.verifyHost({ groupId: params.groupId, hostId, hostToken });
        const groupState = store.getGroupState(params.groupId);
        if (groupState.currentHostId !== hostId) {
          throw new StoreError(403, "Host is not current host for relay client", "not_current_host");
        }
        if (groupState.currentHostGeneration !== hostGeneration) {
          throw new StoreError(409, "Host generation mismatch for relay client", "host_generation_mismatch");
        }
      } catch (error) {
        if (error instanceof StoreError) {
          socket.socket.close(4000 + error.statusCode, error.message);
        } else {
          socket.socket.close(4000, "Internal error");
        }
        return;
      }

      request.log.info({
        event: "host keepalive client attached",
        groupId: params.groupId,
        hostId,
        hostGeneration,
        remoteAddress: request.socket.remoteAddress,
      });
      relay.registerHostClient(params.groupId, hostId, socket.socket);
    },
  );

  app.get(
    "/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/host",
    { websocket: true },
    (socket, request) => {
      const params = parseWSParams(tunnelSessionGetParamsSchema, request, (code, reason) => {
        try { socket.socket.close(code, reason); } catch { socket.destroy(); }
      });
      if (!params) return;

      const hostId = singleHeader(request.headers["x-acbh-host-id"]);
      const hostToken = singleHeader(request.headers["x-acbh-host-token"]);
      const hostGeneration = parseOptionalIntHeader(request.headers["x-acbh-host-generation"]);

      if (!hostId || !hostToken) {
        socket.socket.close(4001, "Host authentication headers are required (X-ACBH-Host-ID, X-ACBH-Host-Token)");
        return;
      }
      if (hostGeneration === null || hostGeneration === undefined) {
        socket.socket.close(4001, "Host generation header is required (X-ACBH-Host-Generation)");
        return;
      }

      try {
        store.verifyHost({ groupId: params.groupId, hostId, hostToken });
        store.getTunnelSessionForRelay(params.groupId, params.sessionId, hostId, hostGeneration);
      } catch (error) {
        if (error instanceof StoreError) {
          socket.socket.close(4000 + error.statusCode, error.message);
        } else {
          socket.socket.close(4000, "Internal error");
        }
        return;
      }

      request.log.info({
        event: "host side attached",
        groupId: params.groupId,
        sessionId: params.sessionId,
        hostId,
        remoteAddress: request.socket.remoteAddress,
      });
      relay.registerHost(params.sessionId, params.groupId, socket.socket);
    },
  );

  app.get(
    "/v1/groups/:groupId/relay/tunnel-sessions/:sessionId/player",
    { websocket: true },
    (socket, request) => {
      const params = parseWSParams(tunnelSessionGetParamsSchema, request, (code, reason) => {
        try { socket.socket.close(code, reason); } catch { socket.destroy(); }
      });
      if (!params) return;

      const playerId = singleHeader(request.headers["x-acbh-player-id"]);
      const playerToken = singleHeader(request.headers["x-acbh-player-token"]);

      if (!playerId || !playerToken) {
        socket.socket.close(4001, "Player authentication headers are required (X-ACBH-Player-ID, X-ACBH-Player-Token)");
        return;
      }

      try {
        store.verifyPlayerToken(params.groupId, playerId, playerToken);

        const session = store.getTunnelSession(params.groupId, params.sessionId);
        if (session.playerId !== playerId) {
          socket.socket.close(4003, "Tunnel session does not belong to this player");
          return;
        }
        if (session.status !== "pending" && session.status !== "active") {
          socket.socket.close(4409, `Tunnel session is ${session.status} and cannot be joined for relay`);
          return;
        }
        const expiresMs = Date.parse(session.expiresAt);
        if (!Number.isFinite(expiresMs) || expiresMs <= Date.now()) {
          socket.socket.close(4410, "Tunnel session has expired");
          return;
        }
      } catch (error) {
        if (error instanceof StoreError) {
          socket.socket.close(4000 + error.statusCode, error.message);
        } else {
          socket.socket.close(4000, "Internal error");
        }
        return;
      }

      request.log.info({
        event: "player side attached",
        groupId: params.groupId,
        sessionId: params.sessionId,
        playerId,
        remoteAddress: request.socket.remoteAddress,
      });
      relay.registerPlayer(params.sessionId, params.groupId, socket.socket);
    },
  );

  app.post("/v1/groups/:groupId/player-sessions", async (request, reply) => {
    const params = parseParams(playerSessionParamsSchema, request, reply);
    const body = parseBody(playerSessionCreateSchema, request, reply);
    if (!params || !body) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      const session = store.createPlayerSession({
        groupId: params.groupId,
        displayName: body.displayName,
      });
      return { ...session };
    });
  });

  app.post("/v1/groups", async (request, reply) => {
    const body = parseBody(createGroupSchema, request, reply);
    if (!body) {
      return reply;
    }

    return store.createGroup(body);
  });

  app.post("/v1/groups/:groupId/join", async (request, reply) => {
    const params = parseParams(joinGroupParamsSchema, request, reply);
    const body = parseBody(joinGroupSchema, request, reply);
    if (!params || !body) {
      return reply;
    }

    return handleStoreCall(reply, () =>
      store.joinGroup({
        groupId: params.groupId,
        accessKey: body.accessKey,
        displayName: body.displayName,
      }),
    );
  });

  app.post("/v1/groups/:groupId/invites", async (request, reply) => {
    const params = parseParams(joinGroupParamsSchema, request, reply);
    const body = parseBody(createInviteSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    return handleStoreCall(reply, () =>
      store.createInvite({
        groupId: params.groupId,
        accessKey: body.accessKey,
        hostId: body.hostId,
        hostToken: body.hostToken,
        expiresInSeconds: body.expiresInSeconds,
        oneTime: body.oneTime,
      }),
    );
  });

  app.post("/v1/groups/:groupId/invites/list", async (request, reply) => {
    const params = parseParams(joinGroupParamsSchema, request, reply);
    const body = parseBody(listInviteSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    return handleStoreCall(reply, () =>
      store.listInvites({
        groupId: params.groupId,
        accessKey: body.accessKey,
        hostId: body.hostId,
        hostToken: body.hostToken,
      }),
    );
  });

  app.post("/v1/groups/:groupId/invites/revoke", async (request, reply) => {
    const params = parseParams(joinGroupParamsSchema, request, reply);
    const body = parseBody(revokeInviteSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    return handleStoreCall(reply, () =>
      store.revokeInvite({
        groupId: params.groupId,
        accessKey: body.accessKey,
        hostId: body.hostId,
        hostToken: body.hostToken,
        inviteId: body.inviteId,
      }),
    );
  });

  app.post("/v1/invites/join", async (request, reply) => {
    const body = parseBody(joinInviteSchema, request, reply);
    if (!body) {
      return reply;
    }
    if (!allowInviteJoinAttempt(request.ip, body.inviteCode)) {
      return reply.code(429).send({
        error: "Too Many Requests",
        message: "Invite join failed. Please try again later.",
      });
    }
    return handleStoreCall(reply, () => store.joinWithInvite(body));
  });

  app.post("/v1/hosts/register", async (request, reply) => {
    const body = parseBody(registerHostSchema, request, reply);
    if (!body) {
      return reply;
    }
    const accessKey = body.accessKey;
    if (!accessKey) {
      return reply.code(401).send({
        error: "Unauthorized",
        message: "Group access key is required",
      });
    }

    return handleStoreCall(reply, () => store.registerHost({ ...body, accessKey }));
  });

  app.post("/v1/hosts/heartbeat", async (request, reply) => {
    const body = parseBody(heartbeatSchema, request, reply);
    if (!body) {
      return reply;
    }

    return handleStoreCall(reply, () => store.updateHeartbeat(body));
  });

  app.get("/v1/groups/:groupId/whoami", async (request, reply) => {
    const params = parseParams(groupStateParamsSchema, request, reply);
    if (!params) {
      return reply;
    }
    const authResult = hostAuthHeaderSchema.safeParse({
      hostId: request.headers["x-acbh-host-id"],
      hostToken: request.headers["x-acbh-host-token"],
    });
    if (!authResult.success) {
      return reply.code(401).send({
        error: "Unauthorized",
        code: "missing_host_token",
        message: "Host authentication headers are required",
        issues: authResult.error.issues,
      });
    }
    return handleStoreCall(reply, () => {
      const whoami = ((store as any).whoAmI ?? (store as any).whoami).bind(store);
      return whoami({ groupId: params.groupId, ...authResult.data });
    });
  });

  app.get("/v1/groups/:groupId/members", async (request, reply) => {
    const params = parseParams(groupStateParamsSchema, request, reply);
    if (!params) {
      return reply;
    }
    const authResult = hostAuthHeaderSchema.safeParse({
      hostId: request.headers["x-acbh-host-id"],
      hostToken: request.headers["x-acbh-host-token"],
    });
    if (!authResult.success) {
      return reply.code(401).send({
        error: "Unauthorized",
        code: "missing_host_token",
        message: "Host authentication headers are required",
        issues: authResult.error.issues,
      });
    }
    return handleStoreCall(reply, () => {
      store.verifyHost({ groupId: params.groupId, ...authResult.data });
      const state = store.getGroupState(params.groupId);
      const localHost = state.hosts.find((host) => host.hostId === authResult.data.hostId);
      return {
        groupId: state.groupId,
        groupName: state.name,
        members: state.members.map((member) => ({
          ...member,
          isLocal: member.memberId === localHost?.memberId,
        })),
      };
    });
  });

  app.get("/v1/groups/:groupId/lease/status", async (request, reply) => {
    const params = parseParams(groupStateParamsSchema, request, reply);
    if (!params) {
      return reply;
    }
    const authResult = hostAuthHeaderSchema.safeParse({
      hostId: request.headers["x-acbh-host-id"],
      hostToken: request.headers["x-acbh-host-token"],
    });
    if (!authResult.success) {
      return reply.code(401).send({
        error: "Unauthorized",
        code: "missing_host_token",
        message: "Host authentication headers are required",
        issues: authResult.error.issues,
      });
    }
    return handleStoreCall(reply, () =>
      store.getHostLeaseStatus({ groupId: params.groupId, ...authResult.data }),
    );
  });

  app.post("/v1/groups/:groupId/lease/ensure-active", async (request, reply) => {
    const params = parseParams(groupStateParamsSchema, request, reply);
    const body = parseBody(leaseEnsureSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    if (params.groupId !== body.groupId) {
      return reply.code(400).send({
        error: "Bad Request",
        message: "Request groupId must match route groupId",
      });
    }
    return handleStoreCall(reply, () => store.ensureActiveLease(body));
  });

  app.post("/v1/groups/:groupId/election/run", async (request, reply) => {
    const params = parseParams(electionParamsSchema, request, reply);
    const body = parseBody(electionRunSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    if (params.groupId !== body.groupId) {
      return reply.code(400).send({
        error: "Bad Request",
        message: "Request groupId must match route groupId",
      });
    }

    return handleStoreCall(reply, () => {
      store.verifyHost(body);
      return store.runElection({ groupId: body.groupId, reason: body.reason });
    });
  });

  app.post("/v1/groups/:groupId/election/check-timeout", async (request, reply) => {
    const params = parseParams(electionParamsSchema, request, reply);
    const body = parseBody(electionAuthSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    if (params.groupId !== body.groupId) {
      return reply.code(400).send({
        error: "Bad Request",
        message: "Request groupId must match route groupId",
      });
    }

    return handleStoreCall(reply, () => {
      store.verifyHost(body);
      return store.checkElectionTimeout(body.groupId);
    });
  });

  app.get("/v1/groups/:groupId/election/status", async (request, reply) => {
    const params = parseParams(electionParamsSchema, request, reply);
    if (!params) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }
      return store.getElectionStatus(params.groupId);
    });
  });

  app.post("/v1/hosts/takeover/poll", async (request, reply) => {
    const body = parseBody(takeoverPollSchema, request, reply);
    if (!body) {
      return reply;
    }
    return handleStoreCall(reply, () => store.pollTakeover(body));
  });

  app.post("/v1/hosts/takeover/accept", async (request, reply) => {
    const body = parseBody(takeoverActionSchema, request, reply);
    if (!body) {
      return reply;
    }
    return handleStoreCall(reply, () => ({
      ok: true,
      assignment: store.acceptTakeover(body),
    }));
  });

  app.post("/v1/hosts/takeover/complete", async (request, reply) => {
    const body = parseBody(takeoverActionSchema, request, reply);
    if (!body) {
      return reply;
    }
    return handleStoreCall(reply, () => ({
      ok: true,
      assignment: store.completeTakeover(body),
    }));
  });

  app.post("/v1/hosts/takeover/fail", async (request, reply) => {
    const body = parseBody(takeoverFailSchema, request, reply);
    if (!body) {
      return reply;
    }
    return handleStoreCall(reply, () => ({
      ok: true,
      assignment: store.failTakeover(body),
    }));
  });

  app.get("/v1/groups/:groupId/state", async (request, reply) => {
    const params = parseParams(groupStateParamsSchema, request, reply);
    if (!params) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      if (!verifyRequestGroupAccess(store, params.groupId, request, reply)) {
        return reply;
      }
      return store.getGroupState(params.groupId);
    });
  });

  app.get("/v1/groups/:groupId/artifacts", async (request, reply) => {
    const params = parseParams(groupArtifactsParamsSchema, request, reply);
    if (!params) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }

      return {
        groupId: params.groupId,
        artifacts: store.listArtifacts(params.groupId),
      };
    });
  });

  app.get("/v1/groups/:groupId/artifacts/latest", async (request, reply) => {
    const params = parseParams(groupArtifactsParamsSchema, request, reply);
    const query = parseQuery(latestArtifactQuerySchema, request, reply);
    if (!params || !query) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }

      return store.getLatestArtifact(params.groupId, query.artifactKind);
    });
  });

  app.get("/v1/groups/:groupId/artifacts/:artifactKind/:artifactId/manifest", async (request, reply) => {
    const params = parseParams(artifactManifestParamsSchema, request, reply);
    if (!params) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }

      store.getArtifact(params.groupId, params.artifactKind, params.artifactId);
      return handleStorageCall(reply, async () => ({
        metadata: store.getArtifact(params.groupId, params.artifactKind, params.artifactId),
        manifest: await storage.readManifest({
          groupId: params.groupId,
          artifactKind: params.artifactKind,
          artifactId: params.artifactId,
        }),
      }));
    });
  });

  app.get("/v1/groups/:groupId/artifacts/objects/:sha256", async (request, reply) => {
    const params = parseParams(objectParamsSchema, request, reply);
    if (!params) {
      return reply;
    }

    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }

      return handleStorageCall(reply, async () => {
        const object = await storage.createObjectReadStream({
          groupId: params.groupId,
          sha256: params.sha256,
        });

        reply.type("application/octet-stream");
        reply.header("content-length", object.size);
        return reply.send(object.stream);
      });
    });
  });

  app.post("/v1/groups/:groupId/world-backups/plan", async (request, reply) => {
    const params = parseParams(worldGroupParamsSchema, request, reply);
    const body = parseBody(worldBackupPlanSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    return handleStoreCall(reply, async () => {
      store.authorizeWorldSnapshotPublish({
        groupId: params.groupId,
        sourceHostId: body.hostId,
        hostId: body.hostId,
        hostToken: body.hostToken,
        hostGeneration: body.hostGeneration,
        parentSnapshotId: body.parentSnapshotId,
      });
      return handleStorageCall(reply, async () => {
        const missingObjects = [];
        let existingCount = 0;
        for (const object of dedupePlanObjects(body.objects)) {
          const exists = await storage.objectExists({ groupId: params.groupId, sha256: object.sha256 });
          if (exists) {
            existingCount++;
          } else {
            missingObjects.push(object);
          }
        }
        return {
          ok: true,
          missingObjects,
          existingCount,
        };
      });
    });
  });

  app.put("/v1/groups/:groupId/world-objects/:sha256", async (request, reply) => {
    const params = parseParams(worldObjectParamsSchema, request, reply);
    const authResult = hostAuthHeaderSchema.safeParse({
      hostId: request.headers["x-acbh-host-id"],
      hostToken: request.headers["x-acbh-host-token"],
    });
    if (!params) {
      return reply;
    }
    if (!authResult.success) {
      return reply.code(401).send({
        error: "Unauthorized",
        message: "Host authentication headers are required",
        issues: authResult.error.issues,
      });
    }
    const bodyStream = request.body;
    if (!(bodyStream instanceof Readable)) {
      return reply.code(400).send({
        error: "Bad Request",
        message: "Request body must be application/octet-stream",
      });
    }
    const declaredLength = parseContentLength(request.headers["content-length"]);
    if (declaredLength !== null && declaredLength > maxObjectBytes) {
      bodyStream.resume();
      return reply.code(413).send({
        error: "Payload Too Large",
        message: `Object upload exceeds configured limit of ${maxObjectBytes} bytes`,
      });
    }
    return handleStoreCall(reply, () => {
      store.verifyHost({ groupId: params.groupId, ...authResult.data });
      return handleStorageCall(reply, async () => {
        const exists = await storage.objectExists({ groupId: params.groupId, sha256: params.sha256 });
        if (exists) {
          bodyStream.resume();
          const existing = await storage.createObjectReadStream({ groupId: params.groupId, sha256: params.sha256 });
          existing.stream.destroy();
          return { ok: true, sha256: params.sha256, exists: true, size: existing.size };
        }
        const saved = await storage.saveObjectFromStream({
          groupId: params.groupId,
          sha256: params.sha256,
          stream: bodyStream,
          maxBytes: maxObjectBytes,
        });
        return { ok: true, sha256: params.sha256, exists: false, size: saved.size };
      });
    });
  });

  app.post("/v1/groups/:groupId/world-backups/commit", { bodyLimit: manifestUploadBodyLimitBytes * 8 }, async (request, reply) => {
    const params = parseParams(worldGroupParamsSchema, request, reply);
    const body = parseBody(worldBackupCommitSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    const manifest = body.manifest as WorldSnapshotManifest;
    if (manifest.groupId !== params.groupId) {
      return reply.code(400).send({ error: "Bad Request", message: "manifest groupId must match route groupId" });
    }
    if (manifest.sourceHostId !== body.hostId || manifest.hostGeneration !== body.hostGeneration) {
      return reply.code(400).send({ error: "Bad Request", message: "manifest source host and generation must match request" });
    }
    const manifestValidation = validateWorldSnapshotRequest(manifest);
    if (manifestValidation) {
      return reply.code(400).send({ error: "Bad Request", message: manifestValidation });
    }

    return handleStoreCall(reply, async () => {
      const publishAuth = {
        groupId: params.groupId,
        sourceHostId: manifest.sourceHostId,
        hostId: body.hostId,
        hostToken: body.hostToken,
        hostGeneration: body.hostGeneration,
        parentSnapshotId: manifest.parentSnapshotId,
      };
      store.authorizeWorldSnapshotPublish(publishAuth);
      return handleStorageCall(reply, async () => {
        for (const file of manifest.files) {
          const sha = file.objectId.slice("sha256:".length);
          const exists = await storage.objectExists({ groupId: params.groupId, sha256: sha });
          if (!exists) {
            return reply.code(400).send({
              error: "Bad Request",
              code: "missing_world_object",
              message: `Missing world object ${sha} for ${file.path}`,
            });
          }
        }
        await storage.saveWorldSnapshotManifest({
          groupId: params.groupId,
          snapshotId: manifest.snapshotId,
          manifest,
          beforeCommit: () => store.authorizeWorldSnapshotPublish(publishAuth),
        });
        const recorded = store.recordArtifact({
          groupId: params.groupId,
          artifactKind: "world-snapshot",
          artifactId: manifest.snapshotId,
          parentArtifactId: manifest.parentSnapshotId ?? null,
          serverPackVersion: null,
          creatorHostId: manifest.sourceHostId,
          createdAt: manifest.createdAt,
          status: "available",
          manifestSha256: sha256(Buffer.from(JSON.stringify(manifest), "utf8")),
          manifestObjectPath: `groups/${params.groupId}/world-backups/${manifest.snapshotId}/manifest.json`,
          fileCount: manifest.fileCount,
          totalBytes: manifest.logicalSize,
          consistent: manifest.consistent,
          pinned: false,
          sourceHostId: manifest.sourceHostId,
          hostGeneration: manifest.hostGeneration,
          uploadedSize: manifest.uploadedSize,
          changedFileCount: manifest.changedFileCount,
          deletedFileCount: manifest.deletedFileCount,
        });
        return { ok: true, snapshotId: recorded.artifactId, status: recorded.status };
      });
    });
  });

  app.get("/v1/groups/:groupId/world-backups/latest", async (request, reply) => {
    const params = parseParams(worldGroupParamsSchema, request, reply);
    const query = parseQuery(worldLatestQuerySchema, request, reply);
    if (!params || !query) {
      return reply;
    }
    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }
      const latest = store.getLatestArtifact(params.groupId, "world-snapshot");
      if (query.consistentOnly && latest.consistent === false) {
        return reply.code(409).send({
          error: "Conflict",
          code: "inconsistent_world_snapshot",
          message: "Latest world snapshot is marked inconsistent",
        });
      }
      return handleStorageCall(reply, async () => ({
        metadata: worldMetadata(latest),
        manifest: await storage.readWorldSnapshotManifest({ groupId: params.groupId, snapshotId: latest.artifactId }),
      }));
    });
  });

  app.get("/v1/groups/:groupId/world-backups", async (request, reply) => {
    const params = parseParams(worldGroupParamsSchema, request, reply);
    if (!params) {
      return reply;
    }
    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }
      return {
        snapshots: store.listArtifacts(params.groupId, "world-snapshot").map(worldMetadata),
      };
    });
  });

  app.get("/v1/groups/:groupId/world-backups/:snapshotId", async (request, reply) => {
    const params = parseParams(worldSnapshotParamsSchema, request, reply);
    if (!params) {
      return reply;
    }
    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }
      const metadata = store.getArtifact(params.groupId, "world-snapshot", params.snapshotId);
      return handleStorageCall(reply, async () => ({
        metadata: worldMetadata(metadata),
        manifest: await storage.readWorldSnapshotManifest({ groupId: params.groupId, snapshotId: params.snapshotId }),
      }));
    });
  });

  app.delete("/v1/groups/:groupId/world-backups/:snapshotId", async (request, reply) => {
    const params = parseParams(worldSnapshotParamsSchema, request, reply);
    if (!params) {
      return reply;
    }
    const hostId = singleHeader(request.headers["x-acbh-host-id"]);
    const hostToken = singleHeader(request.headers["x-acbh-host-token"]);
    if (!hostId || !hostToken) {
      return reply.code(401).send({ error: "Unauthorized", message: "Host authentication headers are required" });
    }
    return handleStoreCall(reply, () => {
      const deleted = store.deleteWorldSnapshot({ groupId: params.groupId, snapshotId: params.snapshotId, hostId, hostToken });
      return handleStorageCall(reply, async () => {
        await storage.deleteWorldSnapshotManifest({ groupId: params.groupId, snapshotId: params.snapshotId });
        return { ok: true, snapshotId: deleted.artifactId };
      });
    });
  });

  app.post("/v1/groups/:groupId/world-backups/:snapshotId/pin", async (request, reply) => {
    const params = parseParams(worldSnapshotParamsSchema, request, reply);
    const body = parseBody(worldBackupPinSchema, request, reply);
    if (!params || !body) {
      return reply;
    }
    return handleStoreCall(reply, () => {
      const pinned = store.pinWorldSnapshot({
        groupId: params.groupId,
        snapshotId: params.snapshotId,
        hostId: body.hostId,
        hostToken: body.hostToken,
        pinned: body.pinned,
      });
      return { ok: true, snapshotId: pinned.artifactId, pinned: pinned.pinned === true };
    });
  });

  app.get("/v1/groups/:groupId/world-objects/:sha256", async (request, reply) => {
    const params = parseParams(worldObjectParamsSchema, request, reply);
    if (!params) {
      return reply;
    }
    return handleStoreCall(reply, () => {
      if (!verifyRequestHost(store, params.groupId, request, reply)) {
        return reply;
      }
      return handleStorageCall(reply, async () => {
        const object = await storage.createObjectReadStream({ groupId: params.groupId, sha256: params.sha256 });
        reply.type("application/octet-stream");
        reply.header("content-length", object.size);
        return reply.send(object.stream);
      });
    });
  });
}

function parseBody<T extends z.ZodTypeAny>(
  schema: T,
  request: FastifyRequest,
  reply: FastifyReply,
): z.infer<T> | null {
  const result = schema.safeParse(request.body);

  if (!result.success) {
    reply.code(400).send({
      error: "Bad Request",
      message: "Invalid request body",
      issues: result.error.issues,
    });
    return null;
  }

  return result.data;
}

function parseParams<T extends z.ZodTypeAny>(
  schema: T,
  request: FastifyRequest,
  reply: FastifyReply,
): z.infer<T> | null {
  const result = schema.safeParse(request.params);

  if (!result.success) {
    reply.code(400).send({
      error: "Bad Request",
      message: "Invalid route parameters",
      issues: result.error.issues,
    });
    return null;
  }

  return result.data;
}

function parseWSParams<T extends z.ZodTypeAny>(
  schema: T,
  request: FastifyRequest,
  closeFn: (code: number, reason: string) => void,
): z.infer<T> | null {
  const result = schema.safeParse(request.params);

  if (!result.success) {
    closeFn(4400, "Invalid route parameters");
    return null;
  }

  return result.data;
}

function parseQuery<T extends z.ZodTypeAny>(
  schema: T,
  request: FastifyRequest,
  reply: FastifyReply,
): z.infer<T> | null {
  const result = schema.safeParse(request.query);

  if (!result.success) {
    reply.code(400).send({
      error: "Bad Request",
      message: "Invalid query string",
      issues: result.error.issues,
    });
    return null;
  }

  return result.data;
}

function handleStoreCall<T>(reply: FastifyReply, call: () => T | Promise<T>): T | Promise<T | FastifyReply> | FastifyReply {
  try {
    const result = call();
    if (result instanceof Promise) {
      return result.catch((error: unknown) => {
        if (error instanceof StoreError) {
          return reply.code(error.statusCode).send({
            error: statusText(error.statusCode),
            message: error.message,
            ...(error.code ? { code: error.code } : {}),
          });
        }
        throw error;
      });
    }
    return result;
  } catch (error) {
    if (error instanceof StoreError) {
      return reply.code(error.statusCode).send({
        error: statusText(error.statusCode),
        message: error.message,
        ...(error.code ? { code: error.code } : {}),
      });
    }

    throw error;
  }
}

function allowInviteJoinAttempt(ip: string, inviteCode: string): boolean {
  const now = Date.now();
  const codeHash = createHash("sha256").update(inviteCode, "utf8").digest("hex").slice(0, 16);
  const keys = [`ip:${ip}`, `invite:${codeHash}`];
  for (const key of keys) {
    const current = inviteJoinRateLimits.get(key);
    if (current && current.resetAt > now && current.count >= 10) {
      return false;
    }
  }
  for (const key of keys) {
    const current = inviteJoinRateLimits.get(key);
    if (!current || current.resetAt <= now) {
      inviteJoinRateLimits.set(key, { count: 1, resetAt: now + 60_000 });
    } else {
      current.count += 1;
    }
  }
  return true;
}

async function handleStorageCall<T>(
  reply: FastifyReply,
  call: () => Promise<T>,
): Promise<T | FastifyReply> {
  try {
    return await call();
  } catch (error) {
    if (error instanceof StoreError) {
      return reply.code(error.statusCode).send({
        error: statusText(error.statusCode),
        message: error.message,
      });
    }
    if (error instanceof StorageError) {
      const statusCode =
        error instanceof StorageObjectTooLargeError
          ? 413
          : error.name === "StorageNotFoundError"
            ? 404
            : 400;
      return reply.code(statusCode).send({
        error: statusText(statusCode),
        message: error.message,
      });
    }

    throw error;
  }
}

function resolveMaxObjectBytes(value = Number(process.env.ACBH_MAX_OBJECT_BYTES ?? defaultMaxObjectBytes)): number {
  if (!Number.isSafeInteger(value) || value < 0) {
    throw new Error("ACBH_MAX_OBJECT_BYTES must be a non-negative safe integer");
  }
  return value;
}

function parseContentLength(value: string | undefined): number | null {
  if (value === undefined) {
    return null;
  }
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : null;
}

function parseOptionalIntHeader(value: string | string[] | undefined): number | null {
  if (value === undefined) return null;
  const raw = Array.isArray(value) ? value[0] : value;
  if (raw === undefined) return null;
  const parsed = Number(raw);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : null;
}

function parseBearerToken(request: FastifyRequest): string | undefined {
  const header = singleHeader(request.headers.authorization);
  if (!header) {
    return undefined;
  }
  const match = /^Bearer\s+(.+)$/i.exec(header);
  return match?.[1]?.trim();
}

function singleHeader(value: string | string[] | undefined): string | undefined {
  if (value === undefined) return undefined;
  if (Array.isArray(value)) return value[0];
  return value;
}

function statusText(statusCode: number): string {
  switch (statusCode) {
    case 400:
      return "Bad Request";
    case 401:
      return "Unauthorized";
    case 403:
      return "Forbidden";
    case 404:
      return "Not Found";
    case 409:
      return "Conflict";
    case 413:
      return "Payload Too Large";
    default:
      return "Error";
  }
}

function decodeBase64(value: string, reply: FastifyReply): Buffer | null {
  if (!isStrictBase64(value)) {
    reply.code(400).send({
      error: "Bad Request",
      message: "contentBase64 must be valid standard base64",
    });
    return null;
  }

  return Buffer.from(value, "base64");
}

function bootstrapPackageDir(): string {
  return process.env.ACBH_BOOTSTRAP_PACKAGE_DIR ?? path.join(process.cwd(), "packages");
}

async function describeBootstrapPackage(
  definition: (typeof bootstrapPackageDefinitions)[number],
  origin: string,
): Promise<{
  id: string;
  version: string;
  filename: string;
  size: number;
  sha256: string;
  signature: string;
  requiredFor: readonly string[];
  available: boolean;
  url: string | null;
}> {
  const filePath = path.join(bootstrapPackageDir(), definition.filename);
  try {
    const [info, data] = await Promise.all([stat(filePath), readFile(filePath)]);
    if (!info.isFile()) {
      throw new Error("not a file");
    }
    return {
      ...definition,
      size: info.size,
      sha256: createHash("sha256").update(data).digest("hex"),
      signature: await readBootstrapSignature(filePath),
      available: true,
      url: `${origin}/v1/bootstrap/packages/${definition.filename}`,
    };
  } catch {
    return {
      ...definition,
      size: 0,
      sha256: "",
      signature: "",
      available: false,
      url: null,
    };
  }
}

async function readBootstrapSignature(filePath: string): Promise<string> {
  try {
    return (await readFile(`${filePath}.sig`, "utf8")).trim();
  } catch {
    return "unsigned-local-package";
  }
}

function isStrictBase64(value: string): boolean {
  if (value === "") {
    return true;
  }
  return /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value);
}

function verifyRequestHost(
  store: InMemoryCoordinatorStore,
  groupId: string,
  request: FastifyRequest,
  reply: FastifyReply,
): boolean {
  const result = hostAuthHeaderSchema.safeParse({
    hostId: request.headers["x-acbh-host-id"],
    hostToken: request.headers["x-acbh-host-token"],
  });

  if (!result.success) {
    reply.code(401).send({
      error: "Unauthorized",
      message: "Host authentication headers are required",
      issues: result.error.issues,
    });
    return false;
  }

  store.verifyHost({
    groupId,
    hostId: result.data.hostId,
    hostToken: result.data.hostToken,
  });

  return true;
}

function verifyRequestPlayer(
  store: InMemoryCoordinatorStore,
  groupId: string,
  playerId: string,
  request: FastifyRequest,
  reply: FastifyReply,
): boolean {
  const headerPlayerId = singleHeader(request.headers["x-acbh-player-id"]);
  const playerToken = singleHeader(request.headers["x-acbh-player-token"]);
  if (!headerPlayerId || !playerToken) {
    reply.code(401).send({
      error: "Unauthorized",
      message: "Player authentication headers are required",
    });
    return false;
  }
  if (headerPlayerId !== playerId) {
    reply.code(403).send({
      error: "Forbidden",
      message: "Player credential does not match this session",
    });
    return false;
  }
  store.verifyPlayerToken(groupId, playerId, playerToken);
  return true;
}

function verifyRequestGroupAccess(
  store: InMemoryCoordinatorStore,
  groupId: string,
  request: FastifyRequest,
  reply: FastifyReply,
): boolean {
  const hostId = singleHeader(request.headers["x-acbh-host-id"]);
  const hostToken = singleHeader(request.headers["x-acbh-host-token"]);

  if (hostId !== undefined || hostToken !== undefined) {
    if (!hostId || !hostToken) {
      reply.code(401).send({
        error: "Unauthorized",
        message: "Complete host authentication headers are required",
      });
      return false;
    }
    store.verifyHost({ groupId, hostId, hostToken });
    return true;
  }

  const accessKey = singleHeader(request.headers["x-acbh-access-key"]);
  if (!accessKey) {
    reply.code(401).send({
      error: "Unauthorized",
      message: "Group access key or host authentication is required",
    });
    return false;
  }

  store.verifyGroupAccessKey(groupId, accessKey);
  return true;
}

function sha256(content: Uint8Array): string {
  return createHash("sha256").update(content).digest("hex");
}

function countIncludedFiles(manifest: { files?: Array<{ deleted?: boolean }> }): number {
  return (manifest.files ?? []).filter((file) => !file.deleted).length;
}

function totalManifestBytes(manifest: { files?: Array<{ deleted?: boolean; size?: number }> }): number {
  return (manifest.files ?? []).reduce((total, file) => {
    if (file.deleted) {
      return total;
    }
    return total + (file.size ?? 0);
  }, 0);
}

function manifestStorageKey(groupId: string, artifactKind: ArtifactKind, artifactId: string): string {
  return `groups/${groupId}/${artifactKind}/${artifactId}/manifest.json`;
}

function dedupePlanObjects(
  objects: Array<{ sha256: string; size: number; path?: string }>,
): Array<{ sha256: string; size: number; path?: string }> {
  const bySha = new Map<string, { sha256: string; size: number; path?: string }>();
  for (const object of objects) {
    const existing = bySha.get(object.sha256);
    if (existing && existing.size !== object.size) {
      throw new StoreError(400, `world object ${object.sha256} has conflicting sizes`);
    }
    if (!existing) {
      bySha.set(object.sha256, object);
    }
  }
  return [...bySha.values()].sort((a, b) => a.sha256.localeCompare(b.sha256));
}

function validateWorldSnapshotRequest(manifest: WorldSnapshotManifest): string | null {
  if (manifest.fileCount !== manifest.files.length) {
    return "fileCount must match files length";
  }
  const deletedPaths = manifest.deletedPaths ?? [];
  if (manifest.deletedFileCount !== deletedPaths.length) {
    return "deletedFileCount must match deletedPaths length";
  }
  let lastPath = "";
  let logicalSize = 0;
  for (const file of manifest.files) {
    if (file.path <= lastPath) {
      return "files must be sorted by path with no duplicates";
    }
    lastPath = file.path;
    if (file.objectId !== `sha256:${file.sha256}`) {
      return "file objectId must match sha256";
    }
    try {
      validateManifestPathForRequest(file.path);
    } catch (error) {
      return error instanceof Error ? error.message : "invalid manifest file path";
    }
    logicalSize += file.size;
  }
  if (logicalSize !== manifest.logicalSize) {
    return "logicalSize must match files total size";
  }
  let lastDeleted = "";
  for (const deletedPath of deletedPaths) {
    if (deletedPath <= lastDeleted) {
      return "deletedPaths must be sorted by path with no duplicates";
    }
    lastDeleted = deletedPath;
    try {
      validateManifestPathForRequest(deletedPath);
    } catch (error) {
      return error instanceof Error ? error.message : "invalid deleted path";
    }
  }
  return null;
}

function validateManifestPathForRequest(value: string): void {
  if (value.length === 0 || value.includes("\\") || path.posix.isAbsolute(value)) {
    throw new Error("manifest path must be a relative POSIX path");
  }
  const normalized = path.posix.normalize(value);
  if (normalized !== value || normalized === "." || normalized.startsWith("../") || normalized.includes("/../")) {
    throw new Error("manifest path must be normalized and must not traverse directories");
  }
}

function worldMetadata(artifact: ArtifactMetadata): {
  snapshotId: string;
  groupId: string;
  status: string;
  profileId: string;
  profileName: string;
  sourceHostId: string;
  serverDir: string | null;
  serverIdentity: string | null;
  hostGeneration: number;
  createdAt: string;
  updatedAt: string;
  completedAt: string | null;
  consistent: boolean;
  pinned: boolean;
  logicalSize: number;
  uploadedSize: number;
  deduplicatedSize: number;
  fileCount: number;
  rootCount: number;
  changedFileCount: number;
  deletedFileCount: number;
  warningCount: number;
  traceId: string | null;
  canRestore: boolean;
  canDownload: boolean;
} {
  const uploadedSize = artifact.uploadedSize ?? 0;
  const status = artifact.status === "available" ? "success" : artifact.status === "rejected" ? "failed" : artifact.status;
  return {
    snapshotId: artifact.artifactId,
    groupId: artifact.groupId,
    status,
    profileId: "minecraft-migratable",
    profileName: "Minecraft 可迁移服务端",
    sourceHostId: artifact.sourceHostId ?? artifact.creatorHostId,
    serverDir: null,
    serverIdentity: artifact.sourceHostId ?? artifact.creatorHostId,
    hostGeneration: artifact.hostGeneration ?? 0,
    createdAt: artifact.createdAt,
    updatedAt: artifact.updatedAt,
    completedAt: artifact.status === "available" ? artifact.updatedAt : null,
    consistent: artifact.consistent !== false,
    pinned: artifact.pinned === true,
    logicalSize: artifact.totalBytes,
    uploadedSize,
    deduplicatedSize: Math.max(artifact.totalBytes - uploadedSize, 0),
    fileCount: artifact.fileCount,
    rootCount: artifact.fileCount > 0 ? 1 : 0,
    changedFileCount: artifact.changedFileCount ?? 0,
    deletedFileCount: artifact.deletedFileCount ?? 0,
    warningCount: artifact.consistent === false ? 1 : 0,
    traceId: null,
    canRestore: artifact.status === "available",
    canDownload: artifact.status === "available",
  };
}
