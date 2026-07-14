#!/usr/bin/env node
import "dotenv/config";
import { Command } from "commander";
import { BitrixApiError, BitrixClient } from "./bitrix/client.js";
import { parsePositiveInteger, requireWebhookUrl, resolveOutputDir } from "./config.js";
import { exportCollection } from "./exporter.js";

const program = new Command()
  .name("athena")
  .description("Export Bitrix24 Knowledge Base 2.0 collections to Markdown and JSON")
  .version("0.1.0");

const auth = program.command("auth").description("Check Bitrix24 authentication");
auth
  .command("check")
  .description("Validate the webhook and note permission")
  .action(async () => {
    const client = createClient();
    const collections = await client.listCollections();
    console.log(`Connected to ${client.portalOrigin}. Accessible collections: ${collections.length}.`);
  });

const collections = program.command("collections").description("Inspect accessible collections");
collections
  .command("list")
  .description("List all Knowledge Base 2.0 collections")
  .action(async () => {
    const client = createClient();
    const items = await client.listCollections();
    if (items.length === 0) {
      console.log("No accessible Knowledge Base 2.0 collections found.");
      return;
    }
    console.table(
      items.map((item) => ({
        id: item.id,
        name: item.name,
        access: item.policyLevel,
        updatedAt: item.updatedAt,
      })),
    );
  });

program
  .command("export")
  .description("Export one Knowledge Base 2.0 collection")
  .option("-c, --collection <id>", "collection ID", parsePositiveInteger, 17)
  .option("-o, --out <directory>", "output directory", "exports/mental-arithmetic")
  .option("--force", "replace an existing complete export", false)
  .action(
    async (options: { collection: number; out: string; force: boolean }) => {
      const client = createClient();
      const summary = await exportCollection(client, {
        collectionId: options.collection,
        outputDir: resolveOutputDir(options.out),
        force: options.force,
        onProgress: (message) => console.log(message),
      });

      console.log("");
      console.log(`Export complete: ${summary.collectionName} (${summary.collectionId})`);
      console.log(`Documents: ${summary.documents}`);
      console.log(
        `Assets: ${summary.assetsDownloaded} downloaded, ${summary.assetsFailed} failed`,
      );
      console.log(`Output: ${summary.outputDir}`);
      if (summary.assetsFailed > 0) {
        console.log("See report.json for resources that could not be downloaded.");
      }
    },
  );

program.showHelpAfterError();

program.parseAsync(process.argv).catch((error: unknown) => {
  const message = formatError(error);
  console.error(`Error: ${message}`);
  process.exitCode = 1;
});

function createClient(): BitrixClient {
  return new BitrixClient(requireWebhookUrl());
}

function formatError(error: unknown): string {
  if (error instanceof BitrixApiError) {
    if (error.code === "insufficient_scope") {
      return `${error.message} Add the note permission to the incoming webhook.`;
    }
    if (error.code === "INVALID_CREDENTIALS" || error.code === "NO_AUTH_FOUND") {
      return `${error.message} Check BITRIX_WEBHOOK_URL in .env.`;
    }
  }
  return error instanceof Error ? error.message : String(error);
}
