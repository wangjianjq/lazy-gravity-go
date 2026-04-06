export namespace config {
	
	export class Config {
	    language: string;
	    telegram_token: string;
	    workspace_base_dir: string;
	    tts_voice: string;
	    allowed_chat_ids: string[];
	    theme: string;
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.telegram_token = source["telegram_token"];
	        this.workspace_base_dir = source["workspace_base_dir"];
	        this.tts_voice = source["tts_voice"];
	        this.allowed_chat_ids = source["allowed_chat_ids"];
	        this.theme = source["theme"];
	    }
	}

}

