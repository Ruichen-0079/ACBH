import { createHash } from "node:crypto";
import { Readable } from "node:stream";
import type { FastifyInstance, FastifyReply, FastifyRequest } from "fastify";
import { z } from "zod";
import type { ArtifactKind } from "./domain/artifacts.js";
import type { CoordinatorStorage } from "./storage/index.js";
import { StorageError, StorageObjectTooLargeError } from "./storage/index.js";
import type { ArtifactManifest } from "./storage/index.js";
import type { InMemoryCoordinatorStore, GcBackend } from "./store.js";
import { StoreError } from "./store.js";
import type { TunnelSession, PlayerSession } from "./network.js";
import type { RelayManager } from "./relay.js";

const jsonObjectUploadDecodedLimitBytes = 16 * 1024 * 1024;
const jsonObjectUploadBodyLimitBytes = 24 * 1024 * 1024;
const manifestUploadBodyLimitBytes = 1024 * 1024;
export const defaultMaxObjectBytes = 256 * 1024 * 1024;

const createGroupSchema = z.object({
  name: z.string().trim().min(1).max(120),
  ownerName: z.string().trim().min(1).max(80),
});

const joinGroupParamsSchema = z.object({
  groupId: z.string().min(1),
});

const joinGroupSchema = z.object({
  accessKey: z.string().min(1),
  displayName: z.string().trim().min(1).max(80),
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

const tunnelSessionCreateSchema = z.object({
  playerId: z.string().min(1),
});

const objectParamsSchema = z.object({
  groupId: z.string().min(1),
  sha256: z.string().regex(/^[a-f0-9]{64}$/),
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
      version: "0.1.0",
    };
  });

  app.get("/v1/info", async () => {
    return {
      name: "ACBH Coordinator",
      mode: "in-memory",
      v1NonGoal: "hot-migration",
      persistence: "none",
    };
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
      store.verifyHost(body);

      if (
        body.manifest.groupId !== body.groupId ||
        body.manifest.artifactKind !== body.artifactKind ||
        body.manifest.artifactId !== body.artifactId
      ) {
        return reply.code(400).send({
          error: "Bad Request",
          message: "Manifest IDs must match upload request",
        });
      }
      if (body.artifactKind === "world-snapshot" && !body.manifest.serverPackVersion) {
        return reply.code(400).send({
          error: "Bad Request",
          message: "serverPackVersion is required for world-snapshot artifacts",
        });
      }
      const uploadedManifest = body.manifest as unknown as ArtifactManifest;

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
        });

        const manifestSha256 = sha256(Buffer.from(JSON.stringify(uploadedManifest), "utf8"));
        const hostGeneration = parseOptionalIntHeader(request.headers["x-acbh-host-generation"]);
        const metadata = store.recordArtifactFromHost({
          metadata: {
            groupId: body.groupId,
            artifactKind: body.artifactKind,
            artifactId: body.artifactId,
            parentArtifactId: uploadedManifest.parentArtifactId ?? null,
            serverPackVersion:
              uploadedManifest.serverPackVersion ?? (body.artifactKind === "server-pack" ? body.artifactId : null),
            creatorHostId: uploadedManifest.creatorHostId,
            createdAt: uploadedManifest.createdAt,
            status: "available",
            manifestSha256,
            manifestObjectPath: manifestStorageKey(body.groupId, body.artifactKind, body.artifactId),
            fileCount: countIncludedFiles(uploadedManifest),
            totalBytes: totalManifestBytes(uploadedManifest),
          },
          hostId: body.hostId,
          hostToken: body.hostToken,
          currentHostGeneration: hostGeneration ?? undefined,
        });

        return {
          ok: true,
          artifactKind: metadata.artifactKind,
          artifactId: metadata.artifactId,
          status: metadata.status,
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
        try {
          const manifest = await storage.readManifest({
            groupId: p.groupId,
            artifactKind: p.artifactKind,
            artifactId: p.artifactId,
          });
          return (manifest.files ?? []).map((f) => ({ sha256: f.sha256, deleted: !!f.deleted }));
        } catch {
          return [];
        }
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
      if (error instanceof StoreError) {
        reply.code(error.statusCode).send({ error: statusText(error.statusCode), message: error.message });
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
      return { ...session };
    });
  });

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

function handleStoreCall<T>(reply: FastifyReply, call: () => T): T | FastifyReply {
  try {
    return call();
  } catch (error) {
    if (error instanceof StoreError) {
      return reply.code(error.statusCode).send({
        error: statusText(error.statusCode),
        message: error.message,
      });
    }

    throw error;
  }
}

async function handleStorageCall<T>(
  reply: FastifyReply,
  call: () => Promise<T>,
): Promise<T | FastifyReply> {
  try {
    return await call();
  } catch (error) {
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

function singleHeader(value: string | string[] | undefined): string | undefined {
  if (value === undefined) return undefined;
  if (Array.isArray(value)) return value[0];
  return value;
}

function statusText(statusCode: number): string {
  switch (statusCode) {
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
