import{U as ve,cP as ge,cQ as me,W as X,aJ as a,bu as Y,V as j,ak as s,bv as J,d as we,X as q,cR as A,o as b,c as x,a as k,_ as l,ar as p,ad as i,n as Q,ac as Z,$ as pe,bK as ye,s as H,am as xe,z as T,aP as O,ci as ke,Z as Se,bz as Ce,a1 as y,a2 as E,cg as c,aS as Be}from"./index-DR-rq6Kt.js";import{u as _e}from"./Input-DQOdvK6o.js";function Re(e){const{primaryColor:d,opacityDisabled:f,borderRadius:n,textColor3:S}=e;return{...ge,iconColor:S,textColor:"white",loadingColor:d,opacityDisabled:f,railColor:"rgba(0, 0, 0, .14)",railColorActive:d,buttonBoxShadow:"0 1px 4px 0 rgba(0, 0, 0, 0.3), inset 0 0 1px 0 rgba(0, 0, 0, 0.05)",buttonColor:"#FFF",railBorderRadiusSmall:n,railBorderRadiusMedium:n,railBorderRadiusLarge:n,buttonBorderRadiusSmall:n,buttonBorderRadiusMedium:n,buttonBorderRadiusLarge:n,boxShadowFocus:`0 0 0 2px ${me(d,{alpha:.2})}`}}const ze={common:ve,self:Re};var $e=X("switch",`
 height: var(--n-height);
 min-width: var(--n-width);
 vertical-align: middle;
 user-select: none;
 -webkit-user-select: none;
 display: inline-flex;
 outline: none;
 justify-content: center;
 align-items: center;
`,[a("children-placeholder",`
 height: var(--n-rail-height);
 display: flex;
 flex-direction: column;
 overflow: hidden;
 pointer-events: none;
 visibility: hidden;
 `),a("rail-placeholder",`
 display: flex;
 flex-wrap: none;
 `),a("button-placeholder",`
 width: calc(1.75 * var(--n-rail-height));
 height: var(--n-rail-height);
 `),X("base-loading",`
 position: absolute;
 top: 50%;
 left: 50%;
 transform: translateX(-50%) translateY(-50%);
 font-size: calc(var(--n-button-width) - 4px);
 color: var(--n-loading-color);
 transition: color .3s var(--n-bezier);
 `,[Y({left:"50%",top:"50%",originalTransform:"translateX(-50%) translateY(-50%)"})]),a("checked, unchecked",`
 transition: color .3s var(--n-bezier);
 color: var(--n-text-color);
 box-sizing: border-box;
 position: absolute;
 white-space: nowrap;
 top: 0;
 bottom: 0;
 display: flex;
 align-items: center;
 line-height: 1;
 `),a("checked",`
 right: 0;
 padding-right: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),a("unchecked",`
 left: 0;
 justify-content: flex-end;
 padding-left: calc(1.25 * var(--n-rail-height) - var(--n-offset));
 `),j("&:focus",[a("rail",`
 box-shadow: var(--n-box-shadow-focus);
 `)]),s("round",[a("rail","border-radius: calc(var(--n-rail-height) / 2);",[a("button","border-radius: calc(var(--n-button-height) / 2);")])]),J("disabled",[J("icon",[s("rubber-band",[s("pressed",[a("rail",[a("button","max-width: var(--n-button-width-pressed);")])]),a("rail",[j("&:active",[a("button","max-width: var(--n-button-width-pressed);")])]),s("active",[s("pressed",[a("rail",[a("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])]),a("rail",[j("&:active",[a("button","left: calc(100% - var(--n-offset) - var(--n-button-width-pressed));")])])])])])]),s("active",[a("rail",[a("button","left: calc(100% - var(--n-button-width) - var(--n-offset))")])]),a("rail",`
 overflow: hidden;
 height: var(--n-rail-height);
 min-width: var(--n-rail-width);
 border-radius: var(--n-rail-border-radius);
 cursor: pointer;
 position: relative;
 transition:
 opacity .3s var(--n-bezier),
 background .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 background-color: var(--n-rail-color);
 `,[a("button-icon",`
 color: var(--n-icon-color);
 transition: color .3s var(--n-bezier);
 font-size: calc(var(--n-button-height) - 4px);
 position: absolute;
 left: 0;
 right: 0;
 top: 0;
 bottom: 0;
 display: flex;
 justify-content: center;
 align-items: center;
 line-height: 1;
 `,[Y()]),a("button",`
 align-items: center; 
 top: var(--n-offset);
 left: var(--n-offset);
 height: var(--n-button-height);
 width: var(--n-button-width-pressed);
 max-width: var(--n-button-width);
 border-radius: var(--n-button-border-radius);
 background-color: var(--n-button-color);
 box-shadow: var(--n-button-box-shadow);
 box-sizing: border-box;
 cursor: inherit;
 content: "";
 position: absolute;
 transition:
 background-color .3s var(--n-bezier),
 left .3s var(--n-bezier),
 opacity .3s var(--n-bezier),
 max-width .3s var(--n-bezier),
 box-shadow .3s var(--n-bezier);
 `)]),s("active",[a("rail","background-color: var(--n-rail-color-active);")]),s("loading",[a("rail",`
 cursor: wait;
 `)]),s("disabled",[a("rail",`
 cursor: not-allowed;
 opacity: .5;
 `)])]);const Ve=["aria-checked","tabindex","onClick","onFocus","onBlur","onKeyup","onKeydown"],Fe={...q.props,size:String,value:{type:[String,Number,Boolean],default:void 0},loading:Boolean,defaultValue:{type:[String,Number,Boolean],default:!1},disabled:{type:Boolean,default:void 0},round:{type:Boolean,default:!0},"onUpdate:value":[Function,Array],onUpdateValue:[Function,Array],checkedValue:{type:[String,Number,Boolean],default:!0},uncheckedValue:{type:[String,Number,Boolean],default:!1},railStyle:Function,rubberBand:{type:Boolean,default:!0},spinProps:Object,onChange:[Function,Array]};let z;var Te=we({name:"Switch",props:Fe,slots:Object,setup(e){z===void 0&&(typeof CSS<"u"?typeof CSS.supports<"u"?z=CSS.supports("width","max(1px)"):z=!1:z=!0);const{mergedClsPrefixRef:d,inlineThemeDisabled:f,mergedComponentPropsRef:n}=pe(e),S=q("Switch","-switch",$e,ze,e,d),v=ye(e,{mergedSize(t){if(e.size!==void 0)return e.size;if(t)return t.mergedSize.value;const w=n?.value?.Switch?.size;return w||"medium"}}),{mergedSizeRef:C,mergedDisabledRef:g}=v,B=H(e.defaultValue),$=Be(e,"value"),m=_e($,B),V=T(()=>m.value===e.checkedValue),o=H(!1),r=H(!1),_=T(()=>{const{railStyle:t}=e;if(t)return t({focused:r.value,checked:V.value})});function F(t){const{"onUpdate:value":w,onChange:P,onUpdateValue:K}=e,{nTriggerFormInput:W,nTriggerFormChange:D}=v;w&&O(w,t),K&&O(K,t),P&&O(P,t),B.value=t,W(),D()}function G(){const{nTriggerFormFocus:t}=v;t()}function ee(){const{nTriggerFormBlur:t}=v;t()}function te(){e.loading||g.value||(m.value!==e.checkedValue?F(e.checkedValue):F(e.uncheckedValue))}function ae(){r.value=!0,G()}function oe(){r.value=!1,ee(),o.value=!1}function ie(t){e.loading||g.value||t.key===" "&&(m.value!==e.checkedValue?F(e.checkedValue):F(e.uncheckedValue),o.value=!1)}function ne(t){e.loading||g.value||t.key===" "&&(t.preventDefault(),o.value=!0)}const L=T(()=>{const{value:t}=C,{self:{opacityDisabled:w,railColor:P,railColorActive:K,buttonBoxShadow:W,buttonColor:D,boxShadowFocus:re,loadingColor:le,textColor:se,iconColor:ce,[y("buttonHeight",t)]:u,[y("buttonWidth",t)]:de,[y("buttonWidthPressed",t)]:ue,[y("railHeight",t)]:h,[y("railWidth",t)]:R,[y("railBorderRadius",t)]:he,[y("buttonBorderRadius",t)]:be},common:{cubicBezierEaseInOut:fe}}=S.value;let M,U,N;return z?(M=`calc((${h} - ${u}) / 2)`,U=`max(${h}, ${u})`,N=`max(${R}, calc(${R} + ${u} - ${h}))`):(M=E((c(h)-c(u))/2),U=E(Math.max(c(h),c(u))),N=c(h)>c(u)?R:E(c(R)+c(u)-c(h))),{"--n-bezier":fe,"--n-button-border-radius":be,"--n-button-box-shadow":W,"--n-button-color":D,"--n-button-width":de,"--n-button-width-pressed":ue,"--n-button-height":u,"--n-height":U,"--n-offset":M,"--n-opacity-disabled":w,"--n-rail-border-radius":he,"--n-rail-color":P,"--n-rail-color-active":K,"--n-rail-height":h,"--n-rail-width":R,"--n-width":N,"--n-box-shadow-focus":re,"--n-loading-color":le,"--n-text-color":se,"--n-icon-color":ce}}),I=f?xe("switch",T(()=>C.value[0]),L,e):void 0;return{handleClick:te,handleBlur:oe,handleFocus:ae,handleKeyup:ie,handleKeydown:ne,mergedRailStyle:_,pressed:o,mergedClsPrefix:d,mergedValue:m,checked:V,mergedDisabled:g,cssVars:f?void 0:L,themeClass:I?.themeClass,onRender:I?.onRender}},render(){const{mergedClsPrefix:e,mergedDisabled:d,checked:f,mergedRailStyle:n,onRender:S,$slots:v}=this;S?.();const{checked:C,unchecked:g,icon:B,"checked-icon":$,"unchecked-icon":m}=v,V=!(A(B)&&A($)&&A(m));return b(),x("div",{role:"switch","aria-checked":f,class:i([`${e}-switch`,this.themeClass,V&&`${e}-switch--icon`,f&&`${e}-switch--active`,d&&`${e}-switch--disabled`,this.round&&`${e}-switch--round`,this.loading&&`${e}-switch--loading`,this.pressed&&`${e}-switch--pressed`,this.rubberBand&&`${e}-switch--rubber-band`]),tabindex:this.mergedDisabled?void 0:0,style:Z(this.cssVars),onClick:this.handleClick,onFocus:this.handleFocus,onBlur:this.handleBlur,onKeyup:this.handleKeyup,onKeydown:this.handleKeydown},[k("div",{class:i(`${e}-switch__rail`),"aria-hidden":"true",style:Z(n)},[l(()=>p(C,o=>p(g,r=>o||r?(b(),x("div",{key:4,"aria-hidden":!0,class:i(`${e}-switch__children-placeholder`)},[k("div",{class:i(`${e}-switch__rail-placeholder`)},[k("div",{class:i(`${e}-switch__button-placeholder`)},null,2),l(()=>o)],2),k("div",{class:i(`${e}-switch__rail-placeholder`)},[k("div",{class:i(`${e}-switch__button-placeholder`)},null,2),l(()=>r)],2)],2)):null))),k("div",{class:i(`${e}-switch__button`)},[l(()=>p(B,o=>p($,r=>p(m,_=>(b(),Q(Ce,null,{default:()=>this.loading?(b(),Q(ke,Se({key:"loading",clsPrefix:e,strokeWidth:20},this.spinProps),null,16,["clsPrefix"])):this.checked&&(r||o)?(b(),x("div",{class:i(`${e}-switch__button-icon`),key:r?"checked-icon":"icon"},[l(()=>r||o)],2)):!this.checked&&(_||o)?(b(),x("div",{class:i(`${e}-switch__button-icon`),key:_?"unchecked-icon":"icon"},[l(()=>_||o)],2)):null},1024)))))),l(()=>p(C,o=>o&&(b(),x("div",{key:"checked",class:i(`${e}-switch__checked`)},[l(()=>o)],2)))),l(()=>p(g,o=>o&&(b(),x("div",{key:"unchecked",class:i(`${e}-switch__unchecked`)},[l(()=>o)],2))))],2)],6)],46,Ve)}});export{Te as S};
