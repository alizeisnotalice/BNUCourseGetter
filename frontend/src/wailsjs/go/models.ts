export namespace main {

	export class CourseRequest {
	    mode: string;
	    speed: number;
	    studentID: string;
	    password: string;
	    courses: selection.Target[];
	    headless: boolean;
	    useWebVpn: boolean;

	    static createFrom(source: any = {}) {
	        return new CourseRequest(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.speed = source["speed"];
	        this.studentID = source["studentID"];
	        this.password = source["password"];
	        this.courses = this.convertValues(source["courses"], selection.Target);
	        this.headless = source["headless"];
	        this.useWebVpn = source["useWebVpn"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace selection {

	export class Target {
	    type: string;
	    courseID: string;
	    classID: string;

	    static createFrom(source: any = {}) {
	        return new Target(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.courseID = source["courseID"];
	        this.classID = source["classID"];
	    }
	}

}
